// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	wishlistv1alpha1 "github.com/lexfrei/wish-operator/api/v1alpha1"
	"github.com/lexfrei/wish-operator/internal/i18n"
	"github.com/lexfrei/wish-operator/internal/templates"

	"golang.org/x/time/rate"
)

const (
	minWeeks = 1
	maxWeeks = 8
)

// Server handles HTTP requests for the wishlist web interface.
type Server struct {
	client    client.Client
	namespace string
	rateLimit float64
	rateBurst int
	limiters  sync.Map
}

// NewServer creates a new web server.
func NewServer(c client.Client, namespace string, rateLimit float64, rateBurst int) *Server {
	return &Server{
		client:    c,
		namespace: namespace,
		rateLimit: rateLimit,
		rateBurst: rateBurst,
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

//nolint:funlen // Main reserve handler with validation logic
func (s *Server) handleReserve(w http.ResponseWriter, r *http.Request) {
	lang := i18n.DetectLanguage(r)
	name := r.PathValue("name")

	if name == "" {
		http.Error(w, i18n.T(lang, "err_missing_name"), http.StatusBadRequest)

		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(lang, "err_invalid_form"), http.StatusBadRequest)

		return
	}

	weeks, err := strconv.Atoi(r.FormValue("weeks"))
	if err != nil || weeks < minWeeks || weeks > maxWeeks {
		http.Error(w, fmt.Sprintf(i18n.T(lang, "err_weeks_range"), minWeeks, maxWeeks), http.StatusBadRequest)

		return
	}

	// Parse quantity (default to 1)
	quantity := int32(1)
	if qStr := r.FormValue("quantity"); qStr != "" {
		q, err := strconv.ParseInt(qStr, 10, 32)
		if err != nil || q < 1 {
			http.Error(w, i18n.T(lang, "err_invalid_quantity"), http.StatusBadRequest)

			return
		}

		quantity = int32(q)
	}

	wish := &wishlistv1alpha1.Wish{}
	if err := s.client.Get(r.Context(), client.ObjectKey{Name: name, Namespace: s.namespace}, wish); err != nil {
		if client.IgnoreNotFound(err) == nil {
			http.Error(w, i18n.T(lang, "err_not_found"), http.StatusNotFound)

			return
		}

		http.Error(w, i18n.T(lang, "err_get_wish"), http.StatusInternalServerError)

		return
	}

	// Check availability using new Reservations model
	available := wish.AvailableQuantity()
	if available == 0 {
		http.Error(w, i18n.T(lang, "err_fully_reserved"), http.StatusConflict)

		return
	}

	if quantity > available {
		http.Error(w, fmt.Sprintf(i18n.T(lang, "err_quantity_exceeds"), available), http.StatusBadRequest)

		return
	}

	// Create new reservation in new format
	now := metav1.Now()
	expires := metav1.NewTime(now.Add(time.Duration(weeks) * 7 * 24 * time.Hour))

	wish.Status.Reservations = append(wish.Status.Reservations, wishlistv1alpha1.Reservation{
		Quantity:  quantity,
		CreatedAt: now,
		ExpiresAt: expires,
	})

	if err := s.client.Status().Update(r.Context(), wish); err != nil {
		http.Error(w, i18n.T(lang, "err_reserve_failed"), http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := templates.WishCard(wish, lang).Render(r.Context(), w); err != nil {
		http.Error(w, i18n.T(lang, "err_render"), http.StatusInternalServerError)
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
		if !wish.Status.Active {
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
	if v, ok := s.limiters.Load(ip); ok {
		limiter, isLimiter := v.(*rate.Limiter)
		if isLimiter {
			return limiter
		}
	}

	limiter := rate.NewLimiter(rate.Limit(s.rateLimit), s.rateBurst)
	s.limiters.Store(ip, limiter)

	return limiter
}

func (s *Server) getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}
