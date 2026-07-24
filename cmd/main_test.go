// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package main

import (
	"context"
	"flag"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	wishlistv1alpha1 "github.com/lexfrei/wish-operator/api/v1alpha1"
)

func newWebFlagSet(t *testing.T) (*flag.FlagSet, *webOptions) {
	t.Helper()

	flagSet := flag.NewFlagSet("test", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	opts := &webOptions{}
	bindWebFlags(flagSet, opts)

	return flagSet, opts
}

func TestBindWebFlags_Defaults(t *testing.T) {
	t.Parallel()

	flagSet, opts := newWebFlagSet(t)
	require.NoError(t, flagSet.Parse(nil))

	assert.Equal(t, "default", opts.namespace)
	assert.InDelta(t, 30.0, opts.rateLimit, 0)
	assert.Equal(t, 10, opts.rateBurst)
	// Proxy headers are forgeable unless an operator declares what sits in
	// front, so the default must not trust them.
	assert.Equal(t, 0, opts.trustedProxyHops)
}

func TestBindWebFlags_Parse(t *testing.T) {
	t.Parallel()

	flagSet, opts := newWebFlagSet(t)
	require.NoError(t, flagSet.Parse([]string{
		"--web-namespace=wishes",
		"--trusted-proxy-hops=2",
		"--rate-limit=5",
		"--rate-burst=3",
	}))

	assert.Equal(t, "wishes", opts.namespace)
	assert.Equal(t, 2, opts.trustedProxyHops)
	assert.InDelta(t, 5.0, opts.rateLimit, 0)
	assert.Equal(t, 3, opts.rateBurst)
}

// mainInvalidFlagHelperEnv makes the re-invoked test binary run main() instead
// of the test body, which is how main's own wiring gets exercised.
const mainInvalidFlagHelperEnv = "TEST_MAIN_INVALID_WEB_FLAGS"

// TestMain_ExitsOnInvalidWebFlags pins that main actually calls the validator
// and refuses to start, rather than validating in a function nobody reaches.
// The child gets a kubeconfig that cannot exist, so if the check were dropped
// it would still exit non-zero — the assertion is on the message, which only
// the validator produces.
func TestMain_ExitsOnInvalidWebFlags(t *testing.T) {
	if os.Getenv(mainInvalidFlagHelperEnv) == "1" {
		// Replace the test binary's arguments so main parses these instead.
		os.Args = []string{"operator", "--rate-limit=NaN"}

		main()

		return
	}

	t.Parallel()

	// os.Args[0] is this very test binary, re-invoked to reach main().
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=TestMain_ExitsOnInvalidWebFlags")
	cmd.Env = append(os.Environ(),
		mainInvalidFlagHelperEnv+"=1",
		"KUBECONFIG=/nonexistent/kubeconfig",
	)

	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError

	require.ErrorAs(t, err, &exitErr, "main must exit non-zero, got output: %s", out)
	assert.Equal(t, 1, exitErr.ExitCode())
	assert.Contains(t, string(out), "--rate-limit must be a finite number greater than zero")
}

// freePort returns an address nothing is listening on. The port could in
// principle be taken between the close and the bind below; nothing else in this
// suite binds ports, so the window is not worth a listener-injection refactor.
func freePort(t *testing.T) string {
	t.Helper()

	var config net.ListenConfig

	listener, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	addr := listener.Addr().String()
	require.NoError(t, listener.Close())

	return addr
}

// TestWebRunnable_StartWaitsForDrain pins that Start does not report the
// runnable as stopped while a request is still being served. Shutdown closes
// the listeners first, so ListenAndServe returns immediately; if Start returned
// then, the manager would move on, the process would exit, and the shutdown
// timeout would bound nothing.
// Not parallel: it binds a port, and running alongside the other bind test
// races for the one freePort just released.
func TestWebRunnable_StartWaitsForDrain(t *testing.T) {
	var (
		handlerEntered = make(chan struct{})
		handlerDone    atomic.Bool
	)

	const handlerWork = 300 * time.Millisecond

	runnable := &webRunnable{
		addr: freePort(t),
		handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(handlerEntered)
			time.Sleep(handlerWork)

			_, _ = w.Write([]byte("done"))

			handlerDone.Store(true)
		}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startErr := make(chan error, 1)

	go func() { startErr <- runnable.Start(ctx) }()

	// Wait for the listener. Firing the request before the bind would fail the
	// connection, and then nothing would ever enter the handler to wait on.
	var dialer net.Dialer

	require.Eventually(t, func() bool {
		conn, err := dialer.DialContext(t.Context(), "tcp", runnable.addr)
		if err != nil {
			return false
		}

		return conn.Close() == nil
	}, 5*time.Second, 10*time.Millisecond, "web server never started listening")

	body := make(chan string, 1)

	go func() {
		resp, err := http.Get("http://" + runnable.addr + "/") //nolint:noctx // the cancel under test is server-side
		if err != nil {
			body <- "request failed: " + err.Error()

			return
		}
		defer func() { _ = resp.Body.Close() }()

		got, err := io.ReadAll(resp.Body)
		if err != nil {
			body <- "read failed: " + err.Error()

			return
		}

		body <- string(got)
	}()

	select {
	case <-handlerEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("request never reached the handler")
	}

	cancel()

	require.NoError(t, <-startErr)
	assert.True(t, handlerDone.Load(), "Start returned while a request was still in flight")
	assert.Equal(t, "done", <-body, "the in-flight response must arrive complete")
}

// TestWebRunnable_StartReturnsBindError pins that the drain wait does not turn
// a failed bind into a hang: ListenAndServe returns before anything can be
// draining, and the shutdown goroutine is still parked on the context.
// Not parallel: it holds a port open for the whole test, see above.
func TestWebRunnable_StartReturnsBindError(t *testing.T) {
	var config net.ListenConfig

	occupied, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer func() { _ = occupied.Close() }()

	runnable := &webRunnable{
		addr:    occupied.Addr().String(),
		handler: http.NewServeMux(),
	}

	startErr := make(chan error, 1)

	go func() { startErr <- runnable.Start(t.Context()) }()

	select {
	case err := <-startErr:
		require.Error(t, err, "a failed bind must be reported, not swallowed")
	case <-time.After(5 * time.Second):
		t.Fatal("Start blocked instead of returning the bind error")
	}
}

// recordingSink captures Error calls so a test can assert the operator was
// told something, rather than only that the code took a branch.
type recordingSink struct {
	mu   sync.Mutex
	errs []string
}

func (s *recordingSink) Init(logr.RuntimeInfo)        {}
func (s *recordingSink) Enabled(int) bool             { return true }
func (s *recordingSink) Info(int, string, ...any)     {}
func (s *recordingSink) WithName(string) logr.LogSink { return s }

func (s *recordingSink) WithValues(...any) logr.LogSink { return s }

func (s *recordingSink) Error(err error, msg string, _ ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.errs = append(s.errs, msg+": "+err.Error())
}

func (s *recordingSink) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.errs)
}

// TestWebRunnable_ReportsShutdownTimeout pins that a drain which runs out of
// time is not silent. The runnable still stops cleanly, so the log line is the
// only signal an operator gets that responses were cut mid-write.
// Not parallel: it binds a port and swaps the package logger, see above.
func TestWebRunnable_ReportsShutdownTimeout(t *testing.T) {
	sink := &recordingSink{}
	original := setupLog
	setupLog = logr.New(sink)

	t.Cleanup(func() { setupLog = original })

	handlerEntered := make(chan struct{})

	runnable := &webRunnable{
		addr: freePort(t),
		handler: http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			close(handlerEntered)
			// Outlast the shutdown budget below.
			time.Sleep(2 * time.Second)
		}),
		shutdownTimeout: 50 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startErr := make(chan error, 1)

	go func() { startErr <- runnable.Start(ctx) }()

	var dialer net.Dialer

	require.Eventually(t, func() bool {
		conn, err := dialer.DialContext(t.Context(), "tcp", runnable.addr)
		if err != nil {
			return false
		}

		return conn.Close() == nil
	}, 5*time.Second, 10*time.Millisecond, "web server never started listening")

	go func() {
		//nolint:noctx // the cancellation under test is server-side
		resp, err := http.Get("http://" + runnable.addr + "/")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()

	select {
	case <-handlerEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("request never reached the handler")
	}

	cancel()

	require.NoError(t, <-startErr, "a timed-out drain still stops the runnable cleanly")

	recorded := sink.recorded()
	require.Len(t, recorded, 1, "the timeout must be reported exactly once")
	assert.Contains(t, recorded[0], "web server shutdown did not drain in time")
	assert.Contains(t, recorded[0], context.DeadlineExceeded.Error())
}

// TestNewWebServer_MapsFlagsOntoTheServer pins the construction seam. Two of
// web.NewServer's parameters are ints, so transposing the hop count and the
// burst compiles, runs, and silently ships a server configured with neither
// value the operator asked for. The values below are picked so that a swap
// changes observable behaviour: with hops 2 the middle entry is the key and one
// token is available, while the swapped pair keys on the last entry and hands
// out two.
func TestNewWebServer_MapsFlagsOntoTheServer(t *testing.T) {
	t.Parallel()

	const (
		proxyAppended = "203.0.113.7"
		innerProxy    = "198.51.100.5"
		watched       = "wishes"
	)

	newServerFor := func(opts *webOptions, wishes ...client.Object) http.Handler {
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(wishes...).
			Build()

		return newWebServer(fakeClient, opts).Handler()
	}

	t.Run("hop count and burst are not transposed", func(t *testing.T) {
		t.Parallel()

		handler := newServerFor(&webOptions{
			namespace:        watched,
			trustedProxyHops: 2,
			rateLimit:        1,
			rateBurst:        1,
		})

		get := func(forged string) int {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			req.RemoteAddr = "10.0.0.1:5000"
			req.Header.Set("X-Forwarded-For", forged+", "+proxyAppended+", "+innerProxy)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			return rec.Code
		}

		require.Equal(t, http.StatusOK, get("1.1.1.1"), "the single burst token should be available")
		assert.Equal(t, http.StatusTooManyRequests, get("2.2.2.2"),
			"both requests key on the entry two hops from the right, and only one token exists")
	})

	t.Run("the watched namespace is passed through", func(t *testing.T) {
		t.Parallel()

		const title = "Namespaced Gift"

		wish := &wishlistv1alpha1.Wish{
			ObjectMeta: metav1.ObjectMeta{Name: "namespaced-wish", Namespace: watched},
			Spec:       wishlistv1alpha1.WishSpec{Title: title},
			Status:     wishlistv1alpha1.WishStatus{Active: true},
		}

		handler := newServerFor(&webOptions{
			namespace:        watched,
			trustedProxyHops: 0,
			rateLimit:        30,
			rateBurst:        10,
		}, wish)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), title)
	})
}

func TestValidateWebFlags(t *testing.T) {
	t.Parallel()

	// Start from a valid set and break one field per case.
	valid := webOptions{namespace: "default", trustedProxyHops: 0, rateLimit: 30, rateBurst: 10}

	tests := []struct {
		name    string
		mutate  func(*webOptions)
		wantErr error
	}{
		{name: "defaults are accepted", mutate: func(*webOptions) {}},
		{name: "one proxy in front", mutate: func(o *webOptions) { o.trustedProxyHops = 1 }},
		// Reads as "trust one hop" but would disable proxy headers instead.
		{
			name:    "negative hops are refused",
			mutate:  func(o *webOptions) { o.trustedProxyHops = -1 },
			wantErr: errNegativeProxyHops,
		},
		// Zero rate or burst never refills and never lets anything through, so
		// every request answers 429 with nothing in the log to explain it.
		{
			name:    "zero rate limit is refused",
			mutate:  func(o *webOptions) { o.rateLimit = 0 },
			wantErr: errRateLimitInvalid,
		},
		{
			name:    "negative rate limit is refused",
			mutate:  func(o *webOptions) { o.rateLimit = -1 },
			wantErr: errRateLimitInvalid,
		},
		// NaN is the one input that fails open: it passes every ordered
		// comparison, so a bare "<= 0" check lets it through, and the limiter
		// then allows every request.
		{
			name:    "NaN rate limit is refused",
			mutate:  func(o *webOptions) { o.rateLimit = math.NaN() },
			wantErr: errRateLimitInvalid,
		},
		{
			name:    "positive infinity is refused",
			mutate:  func(o *webOptions) { o.rateLimit = math.Inf(1) },
			wantErr: errRateLimitInvalid,
		},
		{
			name:    "negative infinity is refused",
			mutate:  func(o *webOptions) { o.rateLimit = math.Inf(-1) },
			wantErr: errRateLimitInvalid,
		},
		{
			name:    "zero burst is refused",
			mutate:  func(o *webOptions) { o.rateBurst = 0 },
			wantErr: errRateBurstTooLow,
		},
		{
			name:    "negative burst is refused",
			mutate:  func(o *webOptions) { o.rateBurst = -1 },
			wantErr: errRateBurstTooLow,
		},
		// An empty namespace is cluster-wide on list, not a narrower scope, so
		// the public page would serve every Wish in the cluster.
		{
			name:    "empty namespace is refused",
			mutate:  func(o *webOptions) { o.namespace = "" },
			wantErr: errEmptyNamespace,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := valid
			tt.mutate(&opts)

			err := validateWebFlags(&opts)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
		})
	}
}

// TestBindWebFlags_RateLimitUnitIsSeconds guards the help text against drifting
// back to "per minute": rate.Limit is events per second, so the wrong unit is a
// 60x error for anyone tuning the limiter from the flag description.
func TestBindWebFlags_RateLimitUnitIsSeconds(t *testing.T) {
	t.Parallel()

	flagSet, _ := newWebFlagSet(t)

	usage := flagSet.Lookup("rate-limit").Usage
	assert.Contains(t, usage, "per second")
	assert.NotContains(t, usage, "per minute")
}
