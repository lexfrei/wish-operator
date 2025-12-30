// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	wishlistv1alpha1 "github.com/lexfrei/wish-operator/api/v1alpha1"
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
	wishes, err := s.listWishes(r.Context())
	if err != nil {
		http.Error(w, "Failed to list wishes", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := templates.Index(wishes).Render(r.Context(), w); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
	}
}

func (s *Server) handleWishes(w http.ResponseWriter, r *http.Request) {
	wishes, err := s.listWishes(r.Context())
	if err != nil {
		http.Error(w, "Failed to list wishes", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := templates.WishList(wishes).Render(r.Context(), w); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
	}
}

func (s *Server) handleReserve(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "Missing wish name", http.StatusBadRequest)

		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)

		return
	}

	weeksStr := r.FormValue("weeks")
	weeks, err := strconv.Atoi(weeksStr)
	if err != nil || weeks < minWeeks || weeks > maxWeeks {
		http.Error(w, fmt.Sprintf("Weeks must be between %d and %d", minWeeks, maxWeeks), http.StatusBadRequest)

		return
	}

	wish := &wishlistv1alpha1.Wish{}
	if err := s.client.Get(r.Context(), client.ObjectKey{Name: name, Namespace: s.namespace}, wish); err != nil {
		if client.IgnoreNotFound(err) == nil {
			http.Error(w, "Wish not found", http.StatusNotFound)

			return
		}

		http.Error(w, "Failed to get wish", http.StatusInternalServerError)

		return
	}

	if wish.Status.Reserved {
		http.Error(w, "Wish is already reserved", http.StatusConflict)

		return
	}

	now := metav1.Now()
	expires := metav1.NewTime(now.Add(time.Duration(weeks) * 7 * 24 * time.Hour))

	wish.Status.Reserved = true
	wish.Status.ReservedAt = &now
	wish.Status.ReservationExpires = &expires

	if err := s.client.Status().Update(r.Context(), wish); err != nil {
		http.Error(w, "Failed to reserve wish", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := templates.WishCard(wish).Render(r.Context(), w); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
	}
}

func (s *Server) listWishes(ctx context.Context) ([]wishlistv1alpha1.Wish, error) {
	wishList := &wishlistv1alpha1.WishList{}
	if err := s.client.List(ctx, wishList, client.InNamespace(s.namespace)); err != nil {
		return nil, err
	}

	// Filter only active wishes
	var active []wishlistv1alpha1.Wish

	for i := range wishList.Items {
		if wishList.Items[i].Status.Active {
			active = append(active, wishList.Items[i])
		}
	}

	return active, nil
}

func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := s.getClientIP(r)
		limiter := s.getLimiter(ip)

		if !limiter.Allow() {
			http.Error(w, "Too many requests", http.StatusTooManyRequests)

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
