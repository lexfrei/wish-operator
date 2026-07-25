// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package web

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	wishlistv1alpha1 "github.com/lexfrei/wish-operator/api/v1alpha1"
	"github.com/lexfrei/wish-operator/internal/i18n"
	"github.com/lexfrei/wish-operator/internal/templates"

	"golang.org/x/time/rate"
)

const (
	minWeeks       = 1
	maxWeeks       = 8
	maxRequestBody = 1 << 20 // 1 MB

	// limiterSweepInterval bounds how often idle limiter entries are swept, so
	// the hot path stays cheap under load.
	limiterSweepInterval = 10 * time.Minute
	// limiterIdleTTL is how long a limiter may sit untouched before eviction.
	limiterIdleTTL = time.Hour
	// limiterMaxEntries caps the map so its size is bounded by more than time.
	// The idle TTL alone lets the map hold every address seen in the last hour,
	// and a directly reachable server sees whatever addresses reach it.
	limiterMaxEntries = 10000
	// limiterEvictTarget is the size a size-triggered eviction trims down to,
	// so the ordering pass is amortised over many requests instead of running
	// once per insert while the map sits at the cap.
	limiterEvictTarget = limiterMaxEntries * 9 / 10
	// ipv6BucketBits is the prefix length IPv6 limiter keys are bucketed to.
	// A /64 is the smallest block a single subscriber is normally assigned.
	ipv6BucketBits = 64
)

// Terminal reservation outcomes. Each maps to a distinct HTTP response.
var (
	errWishNotFound      = errors.New("wish not found")
	errWishExpired       = errors.New("wish is past its TTL")
	errGetWish           = errors.New("cannot read wish")
	errFullyReserved     = errors.New("wish is fully reserved")
	errReservationLimit  = errors.New("wish holds as many reservations as it can")
	errInvalidQuantity   = errors.New("quantity is not a positive number")
	errReserveIncomplete = errors.New("reservation was never attempted")
)

// quantityExceededError reports that fewer items are left than were requested.
type quantityExceededError struct {
	available int32
}

func (e *quantityExceededError) Error() string {
	return fmt.Sprintf("requested quantity exceeds the %d available", e.available)
}

// perRequestLimitError reports that one reservation may not claim this many.
// Distinct from quantityExceededError: an unlimited wish has nothing to run
// out of, so telling its visitor how many are "available" would be false.
type perRequestLimitError struct {
	limit int32
}

func (e *perRequestLimitError) Error() string {
	return fmt.Sprintf("one reservation claims at most %d items", e.limit)
}

// limiterEntry pairs a per-IP limiter with the last time it was accessed, so
// idle entries can be evicted instead of growing the map without bound.
type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// Server handles HTTP requests for the wishlist web interface.
type Server struct {
	client    client.Client
	reader    client.Reader
	namespace string
	rateLimit float64
	rateBurst int

	// trustedProxyHops is how many proxies append to X-Forwarded-For in front
	// of this server. Zero means proxy headers are ignored entirely.
	trustedProxyHops int

	// backoff is the retry schedule for reservation conflicts, a field so a
	// test can inject one that never runs.
	backoff wait.Backoff

	// now is the injectable time source, overridden in tests for deterministic
	// eviction behaviour.
	now func() time.Time

	limitersMu sync.Mutex
	limiters   map[string]*limiterEntry
	lastSweep  time.Time
}

// NewServer creates a new web server. The reader must bypass the informer
// cache: reserving re-reads a wish after a conflict, and a cached read would
// keep handing back the same stale object the conflict was about.
// trustedProxyHops declares how many proxies in front of this server append to
// X-Forwarded-For; use 0 when the server is reachable directly, so that
// client-supplied proxy headers are ignored.
func NewServer(
	c client.Client,
	reader client.Reader,
	namespace string,
	trustedProxyHops int,
	rateLimit float64,
	rateBurst int,
) *Server {
	return &Server{
		client:           c,
		reader:           reader,
		namespace:        namespace,
		trustedProxyHops: trustedProxyHops,
		rateLimit:        rateLimit,
		rateBurst:        rateBurst,
		backoff:          retry.DefaultRetry,
		now:              time.Now,
		limiters:         make(map[string]*limiterEntry),
	}
}

// Handler returns the HTTP handler for the server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /wishes", s.handleWishes)
	mux.HandleFunc("POST /wishes/{name}/reserve", s.handleReserve)

	return s.rateLimitMiddleware(mux)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.renderWishPage(w, r, true)
}

func (s *Server) handleWishes(w http.ResponseWriter, r *http.Request) {
	s.renderWishPage(w, r, false)
}

func (s *Server) renderWishPage(w http.ResponseWriter, r *http.Request, fullPage bool) {
	lang := i18n.DetectLanguage(r)
	filterTag := r.URL.Query().Get("tag")

	wishes, allTags, err := s.listWishes(r.Context(), filterTag)
	if err != nil {
		http.Error(w, i18n.T(lang, "err_list_wishes"), http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	var renderErr error

	if fullPage {
		renderErr = templates.Index(wishes, allTags, filterTag, lang).Render(r.Context(), w)
	} else {
		renderErr = templates.WishContent(wishes, allTags, filterTag, lang).Render(r.Context(), w)
	}

	if renderErr != nil {
		http.Error(w, i18n.T(lang, "err_render"), http.StatusInternalServerError)

		return
	}
}

func (s *Server) handleReserve(w http.ResponseWriter, r *http.Request) {
	lang := i18n.DetectLanguage(r)
	name := r.PathValue("name")

	if name == "" {
		http.Error(w, i18n.T(lang, "err_missing_name"), http.StatusBadRequest)

		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(lang, "err_invalid_form"), http.StatusBadRequest)

		return
	}

	weeks, err := strconv.Atoi(r.FormValue("weeks"))
	if err != nil || weeks < minWeeks || weeks > maxWeeks {
		http.Error(w, fmt.Sprintf(i18n.T(lang, "err_weeks_range"), minWeeks, maxWeeks), http.StatusBadRequest)

		return
	}

	quantity, err := parseQuantity(r.FormValue("quantity"))
	if err != nil {
		writeReserveError(w, lang, err)

		return
	}

	wish, err := s.reserve(r.Context(), name, weeks, quantity)
	if err != nil {
		writeReserveError(w, lang, err)

		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := templates.WishCard(wish, lang).Render(r.Context(), w); err != nil {
		http.Error(w, i18n.T(lang, "err_render"), http.StatusInternalServerError)
	}
}

// reserve appends a reservation to a wish, retrying the whole read-check-write
// cycle whenever the API server rejects the update with a conflict. Every
// attempt re-reads the wish through the uncached reader, so availability is
// checked against the state the reservation is about to be written on top of,
// including the write that caused the conflict.
func (s *Server) reserve(
	ctx context.Context,
	name string,
	weeks int,
	quantity int32,
) (*wishlistv1alpha1.Wish, error) {
	var reserved *wishlistv1alpha1.Wish

	err := retry.RetryOnConflict(s.backoff, func() error {
		wish := &wishlistv1alpha1.Wish{}
		if err := s.reader.Get(ctx, client.ObjectKey{Name: name, Namespace: s.namespace}, wish); err != nil {
			if apierrors.IsNotFound(err) {
				return errWishNotFound
			}

			return fmt.Errorf("%w: %w", errGetWish, err)
		}

		// listWishes hides a wish once its TTL runs out; a POST straight at the
		// name must not be a way around that.
		if wish.IsExpired() {
			return errWishExpired
		}

		if err := checkAvailability(wish, quantity); err != nil {
			return err
		}

		now := metav1.Now()
		expires := metav1.NewTime(now.Add(time.Duration(weeks) * 7 * 24 * time.Hour))

		// Append onto the active ones only: expired entries hold nothing, and
		// writing them back would re-persist state this code does not believe in.
		wish.Status.Reservations = append(wish.ActiveReservations(), wishlistv1alpha1.Reservation{
			Quantity:  quantity,
			CreatedAt: now,
			ExpiresAt: expires,
		})

		if err := s.client.Status().Update(ctx, wish); err != nil {
			return err
		}

		reserved = wish

		return nil
	})
	if err != nil {
		return nil, err
	}

	// A backoff that never runs the closure returns no error and no wish, and
	// the caller would render a nil card. Nothing produces that today, but the
	// only thing standing between here and a panic is a constant in client-go.
	if reserved == nil {
		return nil, errReserveIncomplete
	}

	return reserved, nil
}

// parseQuantity reads the requested quantity, defaulting to a single item.
func parseQuantity(value string) (int32, error) {
	if value == "" {
		return 1, nil
	}

	quantity, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errInvalidQuantity, err)
	}

	if quantity < 1 {
		return 0, errInvalidQuantity
	}

	return int32(quantity), nil
}

// checkAvailability reports whether the wish can still take the requested
// quantity. It answers with the same numbers the card renders, so anything the
// form offers is something this accepts.
func checkAvailability(wish *wishlistv1alpha1.Wish, quantity int32) error {
	if wish.AtReservationLimit() {
		return errReservationLimit
	}

	// A wish with declared stock is bounded by what is left of it, and that is
	// the number worth reporting. The per-request limit only has to speak for
	// unlimited wishes, where nothing else would.
	if !wish.IsUnlimited() {
		available := wish.AvailableQuantity()
		if available == 0 {
			return errFullyReserved
		}

		if quantity > available {
			return &quantityExceededError{available: available}
		}

		return nil
	}

	if quantity > wishlistv1alpha1.MaxQuantityPerRequest {
		return &perRequestLimitError{limit: wishlistv1alpha1.MaxQuantityPerRequest}
	}

	return nil
}

// writeReserveError maps a reservation failure to its HTTP response.
func writeReserveError(w http.ResponseWriter, lang string, err error) {
	var (
		exceeded   *quantityExceededError
		perRequest *perRequestLimitError
	)

	switch {
	// A wish deleted between the read and the write is gone the same way a
	// wish that was never there is gone.
	case errors.Is(err, errWishNotFound), errors.Is(err, errWishExpired), apierrors.IsNotFound(err):
		http.Error(w, i18n.T(lang, "err_not_found"), http.StatusNotFound)
	case errors.Is(err, errInvalidQuantity):
		http.Error(w, i18n.T(lang, "err_invalid_quantity"), http.StatusBadRequest)
	case errors.Is(err, errGetWish):
		http.Error(w, i18n.T(lang, "err_get_wish"), http.StatusInternalServerError)
	case errors.Is(err, errFullyReserved):
		http.Error(w, i18n.T(lang, "err_fully_reserved"), http.StatusConflict)
	case errors.Is(err, errReservationLimit):
		http.Error(w, i18n.T(lang, "err_reservation_limit"), http.StatusConflict)
	case errors.As(err, &exceeded):
		http.Error(w, fmt.Sprintf(i18n.T(lang, "err_quantity_exceeds"), exceeded.available), http.StatusBadRequest)
	case errors.As(err, &perRequest):
		http.Error(w, fmt.Sprintf(i18n.T(lang, "err_quantity_per_request"), perRequest.limit), http.StatusBadRequest)
	case apierrors.IsConflict(err):
		// Retries ran out while the wish was under contention. The status code
		// and Retry-After say so to a machine; the body is the only part a
		// visitor sees, so it must not read like a broken server.
		w.Header().Set("Retry-After", "1")
		http.Error(w, i18n.T(lang, "err_reserve_busy"), http.StatusServiceUnavailable)
	default:
		http.Error(w, i18n.T(lang, "err_reserve_failed"), http.StatusInternalServerError)
	}
}

func (s *Server) listWishes(ctx context.Context, filterTag string) ([]wishlistv1alpha1.Wish, []string, error) {
	wishList := &wishlistv1alpha1.WishList{}
	if err := s.client.List(ctx, wishList, client.InNamespace(s.namespace)); err != nil {
		return nil, nil, err
	}

	// Collect all unique tags and filter active wishes
	tagSet := make(map[string]struct{})
	active := make([]wishlistv1alpha1.Wish, 0, len(wishList.Items))

	for i := range wishList.Items {
		wish := &wishList.Items[i]

		// The same predicate the reserve path uses. Status.Active says the same
		// thing, but only once the controller has caught up: a wish that has
		// never been reconciled would be hidden here while still reservable,
		// and one whose TTL just passed would be listed while already refused.
		if wish.IsExpired() {
			continue
		}

		// Collect all tags for filter UI
		for _, tag := range wish.Spec.Tags {
			tagSet[tag] = struct{}{}
		}

		for _, tag := range wish.Spec.ContextTags {
			tagSet[tag] = struct{}{}
		}

		// Apply tag filter if specified
		if filterTag != "" {
			if !s.wishHasTag(wish, filterTag) {
				continue
			}
		}

		active = append(active, *wish)
	}

	// Sort by priority descending (highest stars first), then by title alphabetically
	sort.Slice(active, func(i, j int) bool {
		if active[i].Spec.Priority != active[j].Spec.Priority {
			return active[i].Spec.Priority > active[j].Spec.Priority
		}

		return active[i].Spec.Title < active[j].Spec.Title
	})

	// Convert tag set to sorted slice
	allTags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		allTags = append(allTags, tag)
	}

	sort.Strings(allTags)

	return active, allTags, nil
}

func (s *Server) wishHasTag(wish *wishlistv1alpha1.Wish, tag string) bool {
	return slices.Contains(wish.Spec.Tags, tag) || slices.Contains(wish.Spec.ContextTags, tag)
}

func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := s.getClientIP(r)
		limiter := s.getLimiter(ip)

		if !limiter.Allow() {
			lang := i18n.DetectLanguage(r)
			http.Error(w, i18n.T(lang, "err_rate_limit"), http.StatusTooManyRequests)

			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) getLimiter(ip string) *rate.Limiter {
	s.limitersMu.Lock()
	defer s.limitersMu.Unlock()

	now := s.now()

	entry, ok := s.limiters[ip]
	if ok {
		entry.lastSeen = now
	} else {
		entry = &limiterEntry{
			limiter:  rate.NewLimiter(rate.Limit(s.rateLimit), s.rateBurst),
			lastSeen: now,
		}
		s.limiters[ip] = entry
	}

	// Sweep after touching this entry so the active caller always survives.
	s.sweepLocked(now)
	s.enforceCapLocked(now, ip)

	return entry.limiter
}

// sweepLocked evicts limiter entries idle longer than limiterIdleTTL. It runs at
// most once per limiterSweepInterval to keep the request path cheap. The caller
// must hold limitersMu.
func (s *Server) sweepLocked(now time.Time) {
	if now.Sub(s.lastSweep) < limiterSweepInterval {
		return
	}

	s.lastSweep = now

	s.evictIdleLocked(now)
}

// evictIdleLocked drops every entry past the idle TTL. The caller must hold
// limitersMu.
func (s *Server) evictIdleLocked(now time.Time) {
	for ip, entry := range s.limiters {
		if now.Sub(entry.lastSeen) > limiterIdleTTL {
			delete(s.limiters, ip)
		}
	}
}

// enforceCapLocked keeps the map under limiterMaxEntries regardless of how much
// time has passed, because the interval gate spreads the sweep cost over time
// without bounding it: between two sweeps the map grows once per new address.
// keep is the entry being served right now, which is never evicted: dropping it
// would hand its caller a limiter that is no longer in the map, so the next
// request would mint a fresh one with a full burst. The caller must hold
// limitersMu.
func (s *Server) enforceCapLocked(now time.Time, keep string) {
	if len(s.limiters) <= limiterMaxEntries {
		return
	}

	// A sweep may not have been due yet, so try the cheap pass first.
	s.evictIdleLocked(now)

	if len(s.limiters) <= limiterMaxEntries {
		return
	}

	// Still over: drop the least recently seen down to the target, so the
	// ordering pass is amortised instead of running once per insert. Ordering
	// alone cannot protect the served entry, because the sort is not stable and
	// timestamps tie whenever the clock is coarser than the arrival rate.
	ips := slices.SortedFunc(maps.Keys(s.limiters), func(a, b string) int {
		return s.limiters[a].lastSeen.Compare(s.limiters[b].lastSeen)
	})

	for _, ip := range ips {
		if len(s.limiters) <= limiterEvictTarget {
			break
		}

		if ip == keep {
			continue
		}

		delete(s.limiters, ip)
	}
}

func (s *Server) getClientIP(r *http.Request) string {
	// Proxy headers only mean something when a known number of proxies sits in
	// front. Each appends the address it received the request from, so counting
	// from the right is the only way to reach an entry no client could write:
	// everything further left is copied verbatim from what the caller sent.
	// With no declared hops the whole header is client-controlled, so it is
	// ignored and the connection address decides.
	if s.trustedProxyHops > 0 {
		// Some proxies add their own X-Forwarded-For line instead of extending
		// the existing one. Both forms carry the same list, so join every line
		// before counting: reading only the first one would count entries in
		// whatever the caller sent and ignore what the proxy wrote.
		//
		// What this needs from the proxy in front: that it appends its view of
		// the downstream address at the END of the list, whether by extending
		// the last line or adding a new one. nginx, Envoy, HAProxy, Traefik and
		// Cloudflare all do. A proxy that prepends instead, or that passes the
		// header through untouched, breaks the count and must be declared as
		// zero hops.
		xff := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
		if ip := trustedForwardedFor(xff, s.trustedProxyHops); ip != "" {
			return ip
		}
	}

	// Declared hops that produced nothing usable mean the request did not come
	// through the chain the operator described, so no header is worth reading:
	// X-Real-IP in particular is passed through untouched by proxies that only
	// append to X-Forwarded-For, and honouring it would let a caller pick its
	// own limiter bucket by sending a malformed X-Forwarded-For alongside it.
	// The connection address goes through the same normalisation as a header
	// entry. This is the path the default configuration takes for every
	// request, so it is where the IPv6 bucketing matters most.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	if key := parseIPEntry(host); key != "" {
		return key
	}

	return host
}

// trustedForwardedFor returns the X-Forwarded-For entry written by the outermost
// trusted proxy. It returns an empty string when the chain is shorter than the
// declared hop count, which means the remaining entries came from the client.
func trustedForwardedFor(xff string, hops int) string {
	if xff == "" {
		return ""
	}

	// Walk right to left rather than splitting the whole header. This runs on
	// caller-supplied input before the rate limiter does anything, so a header
	// full of commas must not turn into one allocation per entry.
	end := len(xff)

	for range hops - 1 {
		comma := strings.LastIndexByte(xff[:end], ',')
		if comma < 0 {
			return ""
		}

		end = comma
	}

	start := strings.LastIndexByte(xff[:end], ',') + 1

	return parseIPEntry(xff[start:end])
}

// parseIPEntry returns value as a bare IP address, or an empty string if it is
// not one. Limiter keys must never be arbitrary caller-supplied text: every
// distinct key mints a limiter with a full burst, so unvalidated keys hand a
// caller both an unbounded map and a way around the limit.
// The key is canonical, so that a proxy writing 2001:0db8::1 on one request and
// 2001:db8::1 on the next does not split one client across two buckets.
func parseIPEntry(value string) string {
	value = strings.TrimSpace(value)

	addr, err := netip.ParseAddr(value)
	if err != nil {
		// Some proxies append host:port rather than a bare address.
		addrPort, portErr := netip.ParseAddrPort(value)
		if portErr != nil {
			return ""
		}

		addr = addrPort.Addr()
	}

	return limiterKey(addr)
}

// limiterKey buckets IPv6 by prefix instead of by address. A single subscriber
// is handed a whole /64, so keying on the address would hand out a fresh
// limiter, with a fresh burst, for every request — the same bypass a forged
// header used to give, reached by rotating addresses instead. IPv4 addresses
// are scarce enough to key individually.
func limiterKey(addr netip.Addr) string {
	addr = addr.Unmap().WithZone("")

	if !addr.Is4() {
		if prefix, err := addr.Prefix(ipv6BucketBits); err == nil {
			return prefix.String()
		}
	}

	return addr.String()
}
