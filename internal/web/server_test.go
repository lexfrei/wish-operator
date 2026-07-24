// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	wishlistv1alpha1 "github.com/lexfrei/wish-operator/api/v1alpha1"

	"golang.org/x/time/rate"
)

// Shared test fixtures to avoid duplicated string literals.
const (
	testNamespace          = "default"
	testWishName           = "test-wish"
	testTitleGift          = "Test Gift"
	testReserveWishName    = "reserve-wish"
	testUnlimitedName      = "unlimited-test"
	testUnlimitedMultiName = "unlimited-test-multi"
	testTitleUnlimited     = "Unlimited Item"
	testProxyHost          = "10.0.0.1"
	testProxyRemoteAddr    = testProxyHost + ":5000"
	testActiveIP           = "203.0.113.1"
	testRealIP             = "203.0.113.9"
	testProxyAppendedIP    = "203.0.113.7"
	testXFFNonIPEntry      = "1.1.1.1, notanip"
	testXFFEmptyEntry      = "1.1.1.1, "
	testIPv6Bucket         = "2001:db8::/64"
	testDirectIP           = "203.0.113.10"
	testDirectRemoteAddr   = testDirectIP + ":5000"
	testUnsplittableAddr   = "not-a-host-port"
)

// newTestServer builds a server with the shipped default hop count, so tests
// that do not care about proxies exercise the configuration the chart produces.
// Proxy behaviour opts in through newTestServerWithHops.
func newTestServer(t *testing.T, wishes ...*wishlistv1alpha1.Wish) *Server {
	t.Helper()

	return newTestServerWithHops(t, 0, wishes...)
}

func newTestServerWithHops(t *testing.T, hops int, wishes ...*wishlistv1alpha1.Wish) *Server {
	t.Helper()

	scheme := runtime.NewScheme()
	err := wishlistv1alpha1.AddToScheme(scheme)
	require.NoError(t, err)

	objs := make([]client.Object, len(wishes))
	for i, w := range wishes {
		objs[i] = w
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(objs...).
		Build()

	return NewServer(fakeClient, testNamespace, hops, 30, 10)
}

func TestServer_HandleIndex(t *testing.T) {
	t.Parallel()

	wish := &wishlistv1alpha1.Wish{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testWishName,
			Namespace: testNamespace,
		},
		Spec: wishlistv1alpha1.WishSpec{
			Title:    testTitleGift,
			Priority: 3,
		},
		Status: wishlistv1alpha1.WishStatus{
			Active: true,
		},
	}

	srv := newTestServer(t, wish)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, rec.Body.String(), testTitleGift)
}

// The CRD sets Minimum=0 on priority and the docs call 0 the unset value, so the
// card must render no star row at all for it. Everything else pins that contract
// as prose; this pins it as behaviour.
func TestServer_HandleIndex_PriorityZeroRendersNoStars(t *testing.T) {
	t.Parallel()

	wish := &wishlistv1alpha1.Wish{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unset-priority",
			Namespace: testNamespace,
		},
		Spec: wishlistv1alpha1.WishSpec{
			Title:    testTitleGift,
			Priority: 0,
		},
		Status: wishlistv1alpha1.WishStatus{
			Active: true,
		},
	}

	srv := newTestServer(t, wish)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), testTitleGift)
	// Assert on the glyphs rather than the class name, which a restyle could
	// rename to "stars filled" and quietly turn this green while stars render.
	assert.NotContains(t, rec.Body.String(), "★", "priority 0 must not render filled stars")
	assert.NotContains(t, rec.Body.String(), "☆", "priority 0 must not render the star row at all")
}

// The zero case above only certifies something if the nonzero cases are pinned
// too: without these, deleting the star row from the template leaves the whole
// suite green and the negative assertion greener still.
func TestServer_HandleIndex_PriorityRendersStars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		priority int32
		expected string
	}{
		{"partial priority fills that many stars", 3, "★★★☆☆"},
		{"maximum priority fills every star", 5, "★★★★★"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			wish := &wishlistv1alpha1.Wish{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "starred-wish",
					Namespace: testNamespace,
				},
				Spec: wishlistv1alpha1.WishSpec{
					Title:    testTitleGift,
					Priority: tt.priority,
				},
				Status: wishlistv1alpha1.WishStatus{
					Active: true,
				},
			}

			srv := newTestServer(t, wish)
			rec := httptest.NewRecorder()

			srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.expected)
		})
	}
}

func TestServer_HandleWishes(t *testing.T) {
	t.Parallel()

	wish := &wishlistv1alpha1.Wish{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testWishName,
			Namespace: testNamespace,
		},
		Spec: wishlistv1alpha1.WishSpec{
			Title:    "HTMX Gift",
			Priority: 5,
		},
		Status: wishlistv1alpha1.WishStatus{
			Active: true,
		},
	}

	srv := newTestServer(t, wish)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/wishes", nil)
	req.Header.Set("Hx-Request", "true")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "HTMX Gift")
}

func TestServer_HandleReserve(t *testing.T) {
	t.Parallel()

	wish := &wishlistv1alpha1.Wish{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testReserveWishName,
			Namespace: testNamespace,
		},
		Spec: wishlistv1alpha1.WishSpec{
			Title:    "Reservable Gift",
			Quantity: 3,
		},
		Status: wishlistv1alpha1.WishStatus{
			Active: true,
		},
	}

	srv := newTestServer(t, wish)
	handler := srv.Handler()

	form := url.Values{}
	form.Set("weeks", "4")
	form.Set("quantity", "2")

	req := httptest.NewRequest(http.MethodPost, "/wishes/reserve-wish/reserve", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify wish now has a reservation
	updatedWish := &wishlistv1alpha1.Wish{}
	err := srv.client.Get(context.Background(),
		client.ObjectKey{Name: testReserveWishName, Namespace: testNamespace},
		updatedWish)
	require.NoError(t, err)
	require.Len(t, updatedWish.Status.Reservations, 1)
	assert.Equal(t, int32(2), updatedWish.Status.Reservations[0].Quantity)

	// Verify reservation is 4 weeks
	expectedExpiry := updatedWish.Status.Reservations[0].CreatedAt.Add(4 * 7 * 24 * time.Hour)
	assert.WithinDuration(t, expectedExpiry, updatedWish.Status.Reservations[0].ExpiresAt.Time, time.Second)
}

func TestServer_HandleReserve_FullyReserved(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	expires := metav1.NewTime(now.Add(7 * 24 * time.Hour))

	wish := &wishlistv1alpha1.Wish{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "reserved-wish",
			Namespace: testNamespace,
		},
		Spec: wishlistv1alpha1.WishSpec{
			Title:    "Fully Reserved Gift",
			Quantity: 2,
		},
		Status: wishlistv1alpha1.WishStatus{
			Active: true,
			Reservations: []wishlistv1alpha1.Reservation{
				{
					Quantity:  2,
					CreatedAt: now,
					ExpiresAt: expires,
				},
			},
		},
	}

	srv := newTestServer(t, wish)
	handler := srv.Handler()

	form := url.Values{}
	form.Set("weeks", "2")

	req := httptest.NewRequest(http.MethodPost, "/wishes/reserved-wish/reserve", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestServer_HandleReserve_QuantityExceedsAvailable(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	expires := metav1.NewTime(now.Add(7 * 24 * time.Hour))

	wish := &wishlistv1alpha1.Wish{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "partial-wish",
			Namespace: testNamespace,
		},
		Spec: wishlistv1alpha1.WishSpec{
			Title:    "Partially Reserved Gift",
			Quantity: 5,
		},
		Status: wishlistv1alpha1.WishStatus{
			Active: true,
			Reservations: []wishlistv1alpha1.Reservation{
				{
					Quantity:  3,
					CreatedAt: now,
					ExpiresAt: expires,
				},
			},
		},
	}

	srv := newTestServer(t, wish)
	handler := srv.Handler()

	form := url.Values{}
	form.Set("weeks", "2")
	form.Set("quantity", "3") // Only 2 available

	req := httptest.NewRequest(http.MethodPost, "/wishes/partial-wish/reserve", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestServer_HandleReserve_InvalidWeeks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		weeks    string
		expected int
	}{
		{"zero weeks", "0", http.StatusBadRequest},
		{"negative weeks", "-1", http.StatusBadRequest},
		{"too many weeks", "9", http.StatusBadRequest},
		{"non-numeric", "abc", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			wish := &wishlistv1alpha1.Wish{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-weeks-wish",
					Namespace: testNamespace,
				},
				Spec: wishlistv1alpha1.WishSpec{
					Title: testTitleGift,
				},
				Status: wishlistv1alpha1.WishStatus{
					Active: true,
				},
			}

			srv := newTestServer(t, wish)
			handler := srv.Handler()

			form := url.Values{}
			form.Set("weeks", tt.weeks)

			req := httptest.NewRequest(http.MethodPost, "/wishes/invalid-weeks-wish/reserve", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			assert.Equal(t, tt.expected, rec.Code)
		})
	}
}

func TestServer_HandleReserve_NotFound(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	handler := srv.Handler()

	form := url.Values{}
	form.Set("weeks", "4")

	req := httptest.NewRequest(http.MethodPost, "/wishes/nonexistent/reserve", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestServer_RateLimiting(t *testing.T) {
	t.Parallel()

	const testRemoteAddr = "192.168.1.1:12345"

	srv := newTestServer(t)
	// Override with very low limits for testing
	// burst=2 means 2 requests can be made immediately
	srv.rateLimit = 1
	srv.rateBurst = 2

	handler := srv.Handler()

	// First request should succeed
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.RemoteAddr = testRemoteAddr
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	assert.Equal(t, http.StatusOK, rec1.Code)

	// Second request should succeed (within burst)
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = testRemoteAddr
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusOK, rec2.Code)

	// Third request should be rate limited (burst exhausted)
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.RemoteAddr = testRemoteAddr
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	assert.Equal(t, http.StatusTooManyRequests, rec3.Code)
}

// TestServer_RateLimitingBehindProxy exercises the contract through the real
// handler chain: two visitors arriving via the same proxy must get their own
// budget, and a caller rotating forged entries must not escape the one it
// already spent.
func TestServer_RateLimitingBehindProxy(t *testing.T) {
	t.Parallel()

	srv := newTestServerWithHops(t, 1)
	srv.rateLimit = 1
	srv.rateBurst = 1

	handler := srv.Handler()

	get := func(xff []string) int {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, newProxiedRequest(xff, testRealIP, testProxyRemoteAddr))

		return rec.Code
	}

	// The forger spends its single token.
	assert.Equal(t, http.StatusOK, get([]string{"1.1.1.1, " + testProxyAppendedIP}))

	// Rotating the caller-controlled prefixes, and moving the forged entry onto
	// its own header line, must land on the same exhausted bucket.
	assert.Equal(t, http.StatusTooManyRequests, get([]string{"2.2.2.2, 3.3.3.3, " + testProxyAppendedIP}))
	assert.Equal(t, http.StatusTooManyRequests, get([]string{"4.4.4.4", testProxyAppendedIP}))

	// An unrelated visitor behind the same proxy still has its own budget.
	assert.Equal(t, http.StatusOK, get([]string{"1.1.1.1, " + testActiveIP}))
}

// TestServer_RotatingHeaderExhaustsOneBudget pins the measured before-state:
// with the old first-entry rule, 5000 requests each carrying a different
// X-Forwarded-For value were all allowed and left 5000 limiters behind. Whether
// the header is trusted or ignored, they must now land in one bucket.
func TestServer_RotatingHeaderExhaustsOneBudget(t *testing.T) {
	t.Parallel()

	const rotations = 5000

	for _, hops := range []int{0, 1} {
		t.Run("hops="+strconv.Itoa(hops), func(t *testing.T) {
			t.Parallel()

			srv := newTestServerWithHops(t, hops)
			srv.rateLimit = 1
			srv.rateBurst = 1

			handler := srv.Handler()

			allowed := 0

			for i := range rotations {
				// Distinct forged prefixes, one appended entry, as a proxy
				// would leave it.
				xff := []string{"198.51.100." + strconv.Itoa(i%256) + ", " + testProxyAppendedIP}

				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, newProxiedRequest(xff, testRealIP, testProxyRemoteAddr))

				if rec.Code == http.StatusOK {
					allowed++
				}
			}

			// One bucket refilling at 1/s: the loop is far too short to earn
			// more than a handful of tokens, and nowhere near 5000.
			assert.Less(t, allowed, 10, "a rotating header must not buy extra budget")
			assert.Len(t, srv.limiters, 1, "a rotating header must not mint limiters")
		})
	}
}

// rateLimit is fed straight into rate.Limit, which counts events per second. The
// --rate-limit flag help used to advertise it as a per-minute budget, off by a
// factor of sixty. Pin the refill interval so the two cannot drift apart again.
func TestServer_RateLimitIsPerSecond(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	srv.rateLimit = 1
	srv.rateBurst = 1

	limiter := srv.getLimiter("192.0.2.1")
	require.True(t, limiter.Allow(), "burst token should be available immediately")

	delay := limiter.Reserve().Delay()
	assert.Less(t, delay, 2*time.Second, "one token per second, not per minute")
	assert.Greater(t, delay, 500*time.Millisecond, "refill should still be throttled")
}

func TestServer_HandleReserve_Unlimited(t *testing.T) {
	t.Parallel()

	wish := &wishlistv1alpha1.Wish{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testUnlimitedName,
			Namespace: testNamespace,
		},
		Spec: wishlistv1alpha1.WishSpec{
			Title:    testTitleUnlimited,
			Quantity: 0, // Unlimited
		},
		Status: wishlistv1alpha1.WishStatus{
			Active: true,
		},
	}

	srv := newTestServer(t, wish)
	handler := srv.Handler()

	// Reserve large quantity
	form := url.Values{}
	form.Set("weeks", "4")
	form.Set("quantity", "100")

	req := httptest.NewRequest(http.MethodPost, "/wishes/unlimited-test/reserve?lang=en", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify reservation was created
	updated := &wishlistv1alpha1.Wish{}
	err := srv.client.Get(context.Background(), client.ObjectKey{Name: testUnlimitedName, Namespace: testNamespace}, updated)
	require.NoError(t, err)

	assert.Len(t, updated.Status.Reservations, 1)
	assert.Equal(t, int32(100), updated.Status.Reservations[0].Quantity)
}

func TestServer_HandleReserve_UnlimitedMultiple(t *testing.T) {
	t.Parallel()

	wish := &wishlistv1alpha1.Wish{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testUnlimitedMultiName,
			Namespace: testNamespace,
		},
		Spec: wishlistv1alpha1.WishSpec{
			Title:    testTitleUnlimited,
			Quantity: 0, // Unlimited
		},
		Status: wishlistv1alpha1.WishStatus{
			Active: true,
		},
	}

	srv := newTestServer(t, wish)
	handler := srv.Handler()

	// First reservation: 50 items
	form1 := url.Values{}
	form1.Set("weeks", "4")
	form1.Set("quantity", "50")

	req1 := httptest.NewRequest(http.MethodPost, "/wishes/unlimited-test-multi/reserve?lang=en", strings.NewReader(form1.Encode()))
	req1.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	assert.Equal(t, http.StatusOK, rec1.Code)

	// Second reservation: 75 items
	form2 := url.Values{}
	form2.Set("weeks", "2")
	form2.Set("quantity", "75")

	req2 := httptest.NewRequest(http.MethodPost, "/wishes/unlimited-test-multi/reserve?lang=en", strings.NewReader(form2.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusOK, rec2.Code)

	// Verify both reservations exist
	final := &wishlistv1alpha1.Wish{}
	err := srv.client.Get(context.Background(), client.ObjectKey{Name: testUnlimitedMultiName, Namespace: testNamespace}, final)
	require.NoError(t, err)

	assert.Len(t, final.Status.Reservations, 2)
	assert.Equal(t, int32(50), final.Status.Reservations[0].Quantity)
	assert.Equal(t, int32(75), final.Status.Reservations[1].Quantity)
}

// newProxiedRequest builds a request carrying one X-Forwarded-For line per
// entry of xff, so tests can cover proxies that add a second line rather than
// extending the first.
func newProxiedRequest(xff []string, xRealIP, remoteAddr string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr

	for _, line := range xff {
		req.Header.Add("X-Forwarded-For", line)
	}

	if xRealIP != "" {
		req.Header.Set("X-Real-IP", xRealIP)
	}

	return req
}

func TestServer_GetClientIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		hops       int
		xff        []string
		xRealIP    string
		remoteAddr string
		want       string
	}{
		{
			// Nothing declared in front means the whole header came from the
			// caller, so honouring it would hand out a bucket per request.
			name:       "undeclared proxy ignores X-Forwarded-For",
			hops:       0,
			xff:        []string{testProxyAppendedIP},
			remoteAddr: testProxyRemoteAddr,
			want:       testProxyHost,
		},
		{
			// A proxy that adds its own line rather than extending the caller's
			// carries the same list; reading only the first line would count
			// entries in what the caller sent.
			name:       "proxy line added after the caller's line is used",
			hops:       1,
			xff:        []string{"1.1.1.1", testProxyAppendedIP},
			remoteAddr: testProxyRemoteAddr,
			want:       testProxyAppendedIP,
		},
		{
			name:       "entries are counted across all header lines",
			hops:       2,
			xff:        []string{"1.1.1.1, " + testProxyAppendedIP, "198.51.100.5"},
			remoteAddr: testProxyRemoteAddr,
			want:       testProxyAppendedIP,
		},
		{
			// Proxies that append to X-Forwarded-For pass X-Real-IP through
			// untouched, so it is caller-controlled at any hop count.
			name:       "X-Real-IP is never trusted",
			hops:       1,
			xRealIP:    testRealIP,
			remoteAddr: testProxyRemoteAddr,
			want:       testProxyHost,
		},
		{
			name:       "X-Real-IP cannot override a rejected X-Forwarded-For",
			hops:       1,
			xff:        []string{testXFFNonIPEntry},
			xRealIP:    testRealIP,
			remoteAddr: testProxyRemoteAddr,
			want:       testProxyHost,
		},
		{
			// A caller rotating fake values only controls the entries to the
			// left of the one the proxy appended.
			name:       "one hop takes the entry the proxy appended",
			hops:       1,
			xff:        []string{"1.1.1.1, 2.2.2.2, " + testProxyAppendedIP},
			remoteAddr: testProxyRemoteAddr,
			want:       testProxyAppendedIP,
		},
		{
			name:       "surrounding whitespace is trimmed",
			hops:       1,
			xff:        []string{"  " + testProxyAppendedIP + "  "},
			remoteAddr: testProxyRemoteAddr,
			want:       testProxyAppendedIP,
		},
		{
			name:       "appended host:port keeps the host",
			hops:       1,
			xff:        []string{"1.1.1.1, " + testProxyAppendedIP + ":41000"},
			remoteAddr: testProxyRemoteAddr,
			want:       testProxyAppendedIP,
		},
		{
			// A single subscriber holds a whole /64, so the bucket is the
			// prefix: keying on the address would hand out a fresh burst for
			// every address the caller cares to use.
			name:       "IPv6 entry is bucketed by prefix",
			hops:       1,
			xff:        []string{"1.1.1.1, 2001:db8::1"},
			remoteAddr: testProxyRemoteAddr,
			want:       testIPv6Bucket,
		},
		{
			// Two spellings of one address must not become two buckets.
			name:       "IPv6 entry is canonicalised",
			hops:       1,
			xff:        []string{"1.1.1.1, 2001:0DB8:0000:0000:0000:0000:0000:0001"},
			remoteAddr: testProxyRemoteAddr,
			want:       testIPv6Bucket,
		},
		{
			name:       "a different IPv6 prefix is a different bucket",
			hops:       1,
			xff:        []string{"1.1.1.1, 2001:db8:0:1::1"},
			remoteAddr: testProxyRemoteAddr,
			want:       "2001:db8:0:1::/64",
		},
		{
			// IPv4 stays per-address; the scarcity argument for bucketing does
			// not apply.
			name:       "IPv4 connection address is not bucketed",
			hops:       0,
			remoteAddr: testDirectRemoteAddr,
			want:       testDirectIP,
		},
		{
			name:       "IPv6 connection address is bucketed too",
			hops:       0,
			remoteAddr: "[2001:db8::dead:beef]:5000",
			want:       testIPv6Bucket,
		},
		{
			// A 4-in-6 mapped address is the IPv4 address, not a v6 prefix.
			name:       "IPv4-mapped IPv6 is treated as IPv4",
			hops:       1,
			xff:        []string{"1.1.1.1, ::ffff:203.0.113.7"},
			remoteAddr: testProxyRemoteAddr,
			want:       testProxyAppendedIP,
		},
		{
			// Anything that is not an address would become a limiter key of the
			// caller's choosing.
			name:       "non-IP entry is rejected",
			hops:       1,
			xff:        []string{testXFFNonIPEntry},
			remoteAddr: testProxyRemoteAddr,
			want:       testProxyHost,
		},
		{
			name:       "empty trailing entry is rejected",
			hops:       1,
			xff:        []string{testXFFEmptyEntry},
			remoteAddr: testProxyRemoteAddr,
			want:       testProxyHost,
		},
		{
			// net/http always sets host:port, so this is unreachable in
			// production. Pinned because it is the only key that does not go
			// through parseIPEntry, and the address comes from the connection
			// rather than from anything the caller wrote.
			name:       "unsplittable RemoteAddr is used as-is",
			hops:       1,
			remoteAddr: testUnsplittableAddr,
			want:       testUnsplittableAddr,
		},
		{
			// Also unreachable through net/http, which hands over a numeric
			// address, but it is the one key that skips address parsing and so
			// worth pinning next to its sibling above.
			name:       "non-numeric RemoteAddr host is used as-is",
			hops:       1,
			remoteAddr: "wish.example:8080",
			want:       "wish.example",
		},
		{
			name:       "no proxy headers falls back to RemoteAddr host",
			hops:       1,
			remoteAddr: testDirectRemoteAddr,
			want:       testDirectIP,
		},
		{
			// With two appending proxies the rightmost entry is the inner
			// proxy's own address, identical for every visitor.
			name:       "two hops skip the inner proxy address",
			hops:       2,
			xff:        []string{"1.1.1.1, " + testProxyAppendedIP + ", 198.51.100.5"},
			remoteAddr: testProxyRemoteAddr,
			want:       testProxyAppendedIP,
		},
		{
			// Fewer entries than declared hops means the chain is not what the
			// operator described, so what is left is caller-supplied.
			name:       "chain shorter than the declared hops falls back",
			hops:       2,
			xff:        []string{testProxyAppendedIP},
			remoteAddr: testProxyRemoteAddr,
			want:       testProxyHost,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServerWithHops(t, tt.hops)
			req := newProxiedRequest(tt.xff, tt.xRealIP, tt.remoteAddr)

			assert.Equal(t, tt.want, srv.getClientIP(req))
		})
	}
}

// TestServer_ForgedProxyHeadersShareOneLimiter pins the property the whole
// change exists for: a caller rotating proxy headers must not mint a limiter
// per value, because each new limiter arrives with a fresh burst and the map
// keeps it for an hour.
func TestServer_ForgedProxyHeadersShareOneLimiter(t *testing.T) {
	t.Parallel()

	rotatingPrefixes := [][]string{
		{"1.1.1.1, " + testProxyAppendedIP},
		{"9.9.9.9, 8.8.8.8, " + testProxyAppendedIP},
		// A caller line the proxy left alone before adding its own.
		{"9.9.9.9", testProxyAppendedIP},
	}

	unparsable := [][]string{
		{testXFFNonIPEntry},
		{"1.1.1.1, <script>"},
		{"1.1.1.1, " + strings.Repeat("a", 64)},
		{testXFFEmptyEntry},
		nil,
	}

	fill := func(srv *Server, headers [][]string) {
		for _, xff := range headers {
			// A caller-supplied X-Real-IP rides along on every request.
			srv.getLimiter(srv.getClientIP(newProxiedRequest(xff, testRealIP, testProxyRemoteAddr)))
		}
	}

	tests := []struct {
		name    string
		hops    int
		headers [][]string
		wantKey string
	}{
		{
			name:    "rotating prefixes reach one limiter",
			hops:    1,
			headers: rotatingPrefixes,
			wantKey: testProxyAppendedIP,
		},
		{
			name:    "unparsable entries collapse onto the connection address",
			hops:    1,
			headers: unparsable,
			wantKey: testProxyHost,
		},
		{
			name:    "undeclared proxy ignores every header",
			hops:    0,
			headers: slices.Concat(rotatingPrefixes, unparsable),
			wantKey: testProxyHost,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServerWithHops(t, tt.hops)
			fill(srv, tt.headers)

			assert.Len(t, srv.limiters, 1, "forged headers must not mint extra limiters")
			assert.Contains(t, srv.limiters, tt.wantKey)
		})
	}
}

// TestServer_IPv6AddressesShareAPrefixBucket pins the v6 half of the same
// property the header rules buy: a caller holding a /64 must not get a fresh
// limiter, and a fresh burst, per address it picks out of it.
func TestServer_IPv6AddressesShareAPrefixBucket(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	limiterFor := func(addr string) *rate.Limiter {
		req := newProxiedRequest(nil, "", addr)

		return srv.getLimiter(srv.getClientIP(req))
	}

	first := limiterFor("[2001:db8::1]:5000")
	second := limiterFor("[2001:db8::dead:beef]:41000")
	other := limiterFor("[2001:db8:0:1::1]:5000")

	assert.Same(t, first, second, "addresses in one /64 must share a limiter")
	assert.NotSame(t, first, other, "a different /64 must get its own limiter")
	assert.Len(t, srv.limiters, 2)
}

func TestServer_GetLimiterIsConcurrencySafe(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	const (
		callers   = 64
		distinct  = 4
		wantEntry = "203.0.113."
	)

	var wg sync.WaitGroup

	limiters := make([]*rate.Limiter, callers)

	for i := range callers {
		wg.Go(func() {
			limiters[i] = srv.getLimiter(wantEntry + strconv.Itoa(i%distinct))
		})
	}

	wg.Wait()

	assert.Len(t, srv.limiters, distinct)

	for i, limiter := range limiters {
		require.NotNil(t, limiter)
		assert.Same(t, limiters[i%distinct], limiter, "one limiter per key")
	}
}

func TestServer_LimiterEviction(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	current := base
	srv.now = func() time.Time { return current }

	// Populate limiters for three distinct IPs at the base time.
	srv.getLimiter(testActiveIP)
	srv.getLimiter("203.0.113.2")
	srv.getLimiter("203.0.113.3")

	require.Len(t, srv.limiters, 3)

	// Advance past both the sweep interval and the idle TTL.
	current = base.Add(limiterIdleTTL + limiterSweepInterval + time.Minute)

	// A request from one IP refreshes it, then triggers the inline sweep that
	// evicts the two now-idle entries.
	srv.getLimiter(testActiveIP)

	require.Len(t, srv.limiters, 1)
	_, ok := srv.limiters[testActiveIP]
	assert.True(t, ok, "active IP must survive the sweep")
}

// TestServer_LimiterSweepIsThrottled pins the interval gate. The sweep walks the
// whole map under the lock, so running it on every request is what the gate
// exists to prevent; without it the O(n) scan lands on every caller.
func TestServer_LimiterSweepIsThrottled(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	current := base
	srv.now = func() time.Time { return current }

	// The first call always sweeps, which is what sets the clock for the gate.
	srv.getLimiter(testActiveIP)
	require.Equal(t, base, srv.lastSweep)

	// Idle entries only ever outlive the TTL by up to one interval, so the gate
	// cannot be observed by an entry that survives; what it controls is how
	// often the map is walked under the lock.
	current = base.Add(limiterSweepInterval - time.Minute)
	srv.getLimiter(testActiveIP)
	assert.Equal(t, base, srv.lastSweep, "sweep must not run again inside the interval")

	current = base.Add(limiterSweepInterval + time.Minute)
	srv.getLimiter(testActiveIP)
	assert.Equal(t, current, srv.lastSweep, "sweep must run once the interval has passed")
}

// TestServer_LimiterSurvivesBelowIdleTTL pins the other side of the boundary:
// an entry idle for slightly less than the TTL is still in use as far as the
// limiter is concerned and must keep its accumulated tokens.
func TestServer_LimiterSurvivesBelowIdleTTL(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	current := base
	srv.now = func() time.Time { return current }

	srv.getLimiter(testActiveIP)
	idle := srv.getLimiter("203.0.113.2")

	// Past the sweep interval so a sweep happens, but short of the idle TTL.
	current = base.Add(limiterIdleTTL - time.Minute)
	srv.getLimiter(testActiveIP)

	require.Len(t, srv.limiters, 2)
	assert.Same(t, idle, srv.getLimiter("203.0.113.2"), "entry below the idle TTL keeps its limiter")
}

// TestServer_LimiterMapIsCapped pins the size bound. The idle TTL only bounds
// the map in time: between two sweeps it grows once per distinct address, and
// nothing about being under the TTL makes an entry worth keeping when there are
// already more of them than the process should hold.
func TestServer_LimiterMapIsCapped(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	current := base
	srv.now = func() time.Time { return current }

	const overflow = 500

	// Every key is fresh and the whole loop stays well inside one sweep
	// interval, so the idle sweep can never fire: only the cap can bound this.
	var lastKey string

	for i := range limiterMaxEntries + overflow {
		current = base.Add(time.Duration(i) * time.Millisecond)
		lastKey = "key-" + strconv.Itoa(i)

		srv.getLimiter(lastKey)
	}

	require.Equal(t, base, srv.lastSweep, "lastSweep must be unchanged since the first call")
	assert.LessOrEqual(t, len(srv.limiters), limiterMaxEntries)
	assert.Contains(t, srv.limiters, lastKey)
}

// TestServer_LimiterCapPrefersIdleEviction pins the cheap path the cap takes
// first: when dropping idle entries alone brings the map back under the cap, no
// ordering pass runs and nothing still in use is touched. Without it the cap
// would trim by age even when every evictable entry was already stale.
func TestServer_LimiterCapPrefersIdleEviction(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	now := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	srv.now = func() time.Time { return now }

	// A sweep happened a minute ago, so the interval gate holds the scheduled
	// one back and only the cap can act. Without that the sweep would clear the
	// map first and this path would never be reached.
	srv.lastSweep = now.Add(-time.Minute)

	idle := make([]string, 0, limiterMaxEntries+1)

	for i := range limiterMaxEntries + 1 {
		key := "idle-" + strconv.Itoa(i)
		idle = append(idle, key)
		srv.limiters[key] = &limiterEntry{
			limiter:  rate.NewLimiter(1, 1),
			lastSeen: now.Add(-2 * limiterIdleTTL),
		}
	}

	fresh := "fresh"
	srv.limiters[fresh] = &limiterEntry{limiter: rate.NewLimiter(1, 1), lastSeen: now}

	srv.getLimiter(testActiveIP)

	assert.LessOrEqual(t, len(srv.limiters), limiterMaxEntries)
	// The survivors are exactly the entries that were not idle, which is what
	// separates idle eviction from a plain trim of the oldest.
	assert.Contains(t, srv.limiters, fresh)
	assert.Contains(t, srv.limiters, testActiveIP)

	for _, key := range idle {
		require.NotContains(t, srv.limiters, key)
	}
}

// TestServer_LimiterCapKeepsTheServedEntry pins that eviction never drops the
// caller it is currently serving. Ordering alone does not guarantee it: the
// sort is not stable and timestamps tie whenever the clock is coarser than the
// arrival rate. An evicted caller is handed a limiter that is no longer in the
// map, so its next request mints a fresh one with a full burst — the bypass
// this change exists to close, reached through cap pressure instead of header
// rotation.
func TestServer_LimiterCapKeepsTheServedEntry(t *testing.T) {
	t.Parallel()

	frozen := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	// Make the served entry strictly the oldest, so the ordering puts it first
	// in line for eviction and only the guard can save it. Ties would leave the
	// outcome to map iteration order, which is randomised — a test that only
	// catches the bug some of the time is not a test.
	t.Run("survives being the oldest entry", func(t *testing.T) {
		t.Parallel()

		srv := newTestServer(t)
		srv.now = func() time.Time { return frozen }

		const served = "served"

		for i := range limiterMaxEntries + 1 {
			srv.limiters["key-"+strconv.Itoa(i)] = &limiterEntry{
				limiter:  rate.NewLimiter(1, 1),
				lastSeen: frozen.Add(time.Second),
			}
		}

		srv.limiters[served] = &limiterEntry{limiter: rate.NewLimiter(1, 1), lastSeen: frozen}

		srv.enforceCapLocked(frozen, served)

		assert.Contains(t, srv.limiters, served, "the entry being served must never be evicted")
		// Trimming to the target rather than to the cap is what amortises the
		// ordering pass; stopping at the cap would re-sort on every insert.
		assert.Len(t, srv.limiters, limiterEvictTarget)
	})

	// The realistic shape: a clock coarser than the arrival rate ties every
	// timestamp, so eviction order among them is arbitrary.
	t.Run("survives a tie across every entry", func(t *testing.T) {
		t.Parallel()

		srv := newTestServer(t)
		srv.now = func() time.Time { return frozen }

		for i := range limiterMaxEntries + 500 {
			key := "key-" + strconv.Itoa(i)

			limiter := srv.getLimiter(key)

			entry, ok := srv.limiters[key]
			require.True(t, ok, "served key %s was evicted while being served", key)
			require.Same(t, limiter, entry.limiter, "served key %s must map to the limiter handed out", key)
		}

		assert.LessOrEqual(t, len(srv.limiters), limiterMaxEntries)
	})
}
