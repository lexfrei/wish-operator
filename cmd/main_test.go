// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
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
	"sigs.k8s.io/controller-runtime/pkg/manager"

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

		return newWebServer(fakeClient, fakeClient, opts).Handler()
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

// Documentation surfaces asserted against the code they describe.
const (
	docReadme   = "../README.md"
	docValues   = "../charts/wish-operator/values.yaml"
	docSchema   = "../charts/wish-operator/values.schema.json"
	docClaude   = "../CLAUDE.md"
	docAPITypes = "../api/v1alpha1/wish_types.go"
	docCRD      = "../charts/wish-operator/crds/wishlist.k8s.lex.la_wishes.yaml"

	// Paths relative to this package; tests run in cmd/.
	docFlagSource = "./main.go"
	docMakefile   = "../Makefile"
	docLintConfig = "../.golangci.yaml"
)

// A package behind a build tag is not a package for anything walking ./..., so
// tagging test/e2e silently removed its only compile check: a broken file there
// passed golangci-lint and go vet alike. run.build-tags puts it back. Assert
// every tag in the tree is listed, so the next one cannot drop out the same way.
func TestBuildTagsAreLinted(t *testing.T) {
	t.Parallel()

	lintConfig, err := os.ReadFile(docLintConfig)
	require.NoError(t, err)

	tags := map[string]bool{}

	require.NoError(t, filepath.WalkDir("..", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return walkErr
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", path, readErr)
		}

		for line := range strings.SplitSeq(string(content), "\n") {
			if !strings.HasPrefix(line, "//go:build ") {
				continue
			}

			constraint := strings.TrimSpace(strings.TrimPrefix(line, "//go:build "))

			// Only a bare positive tag excludes the file by default and so needs
			// to be enabled for linting. A negated or compound constraint —
			// "!ignore_autogenerated" on the generated deepcopy, for instance —
			// is already satisfied without any flag.
			if strings.ContainsAny(constraint, "!&|() ") {
				continue
			}

			tags[constraint] = true
		}

		return nil
	}))

	for tag := range tags {
		assert.Contains(t, string(lintConfig), "- "+tag,
			"%s is behind //go:build %s but the tag is not in run.build-tags, so the package is not linted",
			docLintConfig, tag)
	}
}

// README.md and CLAUDE.md both tell a newcomer which commands to run, and both
// have carried commands that do nothing: "go generate ./..." has no directives
// to run, and "go test ./..." fails without the envtest binaries that only
// make test fetches. Check every documented make target against the Makefile,
// in both files — fixing one and leaving the other is how this drifted.
func TestDocumentedMakeTargetsExist(t *testing.T) {
	t.Parallel()

	makefile, err := os.ReadFile(docMakefile)
	require.NoError(t, err)

	for _, doc := range []string{docClaude, docReadme} {
		content, readErr := os.ReadFile(doc)
		require.NoError(t, readErr)

		assert.NotContains(t, string(content), "go generate",
			"%s: go generate does nothing here; document the make targets instead", doc)
		assert.NotContains(t, string(content), "go test ./...",
			"%s: go test ./... needs KUBEBUILDER_ASSETS; document make test instead", doc)

		var (
			documented int
			inFence    bool
		)

		for line := range strings.SplitSeq(string(content), "\n") {
			if strings.HasPrefix(line, "```") {
				inFence = !inFence

				continue
			}

			// Only inside a fenced block: a prose line like "make sure the CRDs
			// are applied" would otherwise send this looking for a "sure" target.
			if !inFence {
				continue
			}

			fields := strings.Fields(line)
			if len(fields) < 2 || fields[0] != "make" {
				continue
			}

			// "make generate manifests" documents two targets, not one, but
			// "make install    # Install CRDs" documents one and then prose.
			for _, target := range fields[1:] {
				if strings.HasPrefix(target, "#") {
					break
				}

				documented++

				assert.Contains(t, string(makefile), "\n"+target+":",
					"%s documents make %s but the Makefile has no such target", doc, target)
			}
		}

		assert.NotZero(t, documented, "%s should document how to build and test", doc)
	}
}

// The chart ships its own copy of the CRD and that copy, not config/crd/bases,
// is what a Helm install applies. The manifests target keeps them equal; this
// asserts the result reached the commit, so a regenerate-and-forget cannot ship
// users a CRD that prunes fields they just set.
//
// Note it bites in CI, which runs go test directly. Locally, make test depends
// on manifests, so the copy is resynced moments before this compares it.
func TestChartCRDMatchesGeneratedCRD(t *testing.T) {
	t.Parallel()

	generatedDir, err := os.ReadDir("../config/crd/bases")
	require.NoError(t, err)

	shippedDir, err := os.ReadDir("../charts/wish-operator/crds")
	require.NoError(t, err)

	// Compare the file sets, not one hardcoded name: renaming or dropping a CRD
	// would otherwise leave a stale copy behind that Helm still installs.
	generatedNames := make([]string, 0, len(generatedDir))
	shippedNames := make([]string, 0, len(shippedDir))

	for _, entry := range generatedDir {
		generatedNames = append(generatedNames, entry.Name())
	}

	for _, entry := range shippedDir {
		shippedNames = append(shippedNames, entry.Name())
	}

	require.Equal(t, generatedNames, shippedNames,
		"the chart's crds/ must hold exactly the generated CRDs; run make manifests to resync")

	for _, name := range generatedNames {
		generated, readErr := os.ReadFile("../config/crd/bases/" + name)
		require.NoError(t, readErr)

		shipped, readErr := os.ReadFile("../charts/wish-operator/crds/" + name)
		require.NoError(t, readErr)

		assert.Equal(t, string(generated), string(shipped),
			"%s differs from the generated copy; run make manifests to resync", name)
	}
}

// The CRD accepts priority 0 and the card template hides the stars for it, so 0
// is the unset value rather than an invalid one. The docs claimed a 1-5 range,
// which contradicted the minimum shipped in the CRD itself.
func TestPriorityRangeDocumentedWithZero(t *testing.T) {
	t.Parallel()

	docs := []string{docReadme, docClaude, docAPITypes, docCRD}

	for _, path := range docs {
		content, err := os.ReadFile(path)
		require.NoError(t, err)

		// Only lines that talk about priority: an unrelated 1-5 elsewhere, a step
		// or port range, is none of this test's business.
		for line := range strings.SplitSeq(string(content), "\n") {
			if !strings.Contains(strings.ToLower(line), "priority") {
				continue
			}

			assert.NotContains(t, line, "1-5",
				"%s claims a priority range the CRD does not enforce: %s", path, line)
		}
	}

	// Pin the bound itself, not just the prose about it. Flipping Minimum to 1
	// would otherwise leave every document reading "0-5" while the API rejects 0.
	crd, err := os.ReadFile(docCRD)
	require.NoError(t, err)

	var (
		inPriority bool
		minimum    string
	)

	for line := range strings.SplitSeq(string(crd), "\n") {
		trimmed := strings.TrimSpace(line)

		switch {
		case trimmed == "priority:":
			inPriority = true
		case inPriority && strings.HasPrefix(trimmed, "minimum:"):
			minimum = strings.TrimSpace(strings.TrimPrefix(trimmed, "minimum:"))
			inPriority = false
		}
	}

	assert.Equal(t, "0", minimum, "the CRD must accept priority 0 as the unset value")
}

// The replica constraint is enforced by a template fail() and described to users
// in three separate files. values.schema.json is the one ArtifactHub renders, so
// leaving it out hides the constraint exactly where a user browses the chart.
//
// Match the qualified "operator.leaderElection" rather than the bare key, which
// both chart files already carried as a value and would make this vacuous, and
// pin the schema's replicaCount description directly, since that is the field
// whose rendering this test exists to protect.
func TestReplicaConstraintDocumented(t *testing.T) {
	t.Parallel()

	docs := []string{docReadme, docValues, docSchema}

	for _, path := range docs {
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(content), "operator.leaderElection",
			"%s must document the replicaCount constraint", path)
	}

	schema, err := os.ReadFile(docSchema)
	require.NoError(t, err)

	var parsed struct {
		Properties struct {
			ReplicaCount struct {
				Description string `json:"description"`
			} `json:"replicaCount"`
		} `json:"properties"`
	}

	require.NoError(t, json.Unmarshal(schema, &parsed))
	assert.Contains(t, parsed.Properties.ReplicaCount.Description, "operator.leaderElection",
		"the replicaCount description is what ArtifactHub renders")
}

// The web UI has to answer on every replica. controller-runtime files any Runnable
// that does not implement LeaderElectionRunnable into the leader-election group, so
// once leader election is enabled, an implicit opt-in leaves :8080 unbound on every
// non-leader pod while the health probe still reports ready and the Service keeps
// routing traffic there. Single-replica installs never notice, which is what makes
// it worth pinning here.
func TestWebRunnableDoesNotNeedLeaderElection(t *testing.T) {
	t.Parallel()

	var runnable manager.Runnable = &webRunnable{}

	elective, ok := runnable.(manager.LeaderElectionRunnable)
	require.True(t, ok, "webRunnable must implement manager.LeaderElectionRunnable")
	assert.False(t, elective.NeedLeaderElection(), "web server must run on non-leader replicas too")
}

// The linker accepts -X for a symbol that does not exist without complaining, so a
// rename on either side drops the build metadata silently. Referencing the variables
// here means the Go side cannot be renamed without updating this test, and the string
// match means the Containerfile side cannot drift away from it either.
func TestContainerfileLdflagsMatchBuildVariables(t *testing.T) {
	t.Parallel()

	containerfile, err := os.ReadFile("../Containerfile")
	require.NoError(t, err)

	assert.Equal(t, "development", Version, "default must survive a build without -ldflags")
	assert.Equal(t, "development", GitSHA, "default must survive a build without -ldflags")
	assert.Contains(t, string(containerfile), "-X main.Version=")
	assert.Contains(t, string(containerfile), "-X main.GitSHA=")
}

// The manager is built with no Cache options and the controller registers no
// namespace predicate, so it reconciles Wishes cluster-wide; --web-namespace
// scopes only the web UI's List and the reserve handler's Get. All four places
// used to say "namespace to watch", which reads as a blast-radius guarantee the
// operator does not give. Pin the correction wherever the value is described.
func TestWebNamespaceDocumentedAsWebUIOnly(t *testing.T) {
	t.Parallel()

	docs := []string{docFlagSource, docReadme, docValues, docSchema}

	for _, path := range docs {
		content, err := os.ReadFile(path)
		require.NoError(t, err)

		assert.NotContains(t, string(content), "to watch for Wish",
			"%s presents the web namespace as the reconcile scope", path)
		assert.Contains(t, string(content), "all namespaces",
			"%s must say the controller reconciles beyond the web namespace", path)
	}
}

// The rate limit is a per-second rate written down in four places, and three of
// them said "per minute" for a while without anything going red, because every
// test pinned behaviour and none read the prose. Assert the right unit is present
// as well as the wrong one absent, so deleting a description does not pass either.
func TestRateLimitDocumentedAsPerSecond(t *testing.T) {
	t.Parallel()

	docs := []string{docFlagSource, docReadme, docValues, docSchema}

	for _, path := range docs {
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.NotContains(t, string(content), "per minute", "%s documents the rate limit in the wrong unit", path)
		assert.Contains(t, string(content), "per second", "%s must state the rate limit unit", path)
	}
}
