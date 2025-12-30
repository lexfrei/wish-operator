// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	wishlistv1alpha1 "github.com/lexfrei/wish-operator/api/v1alpha1"
)

func newTestServer(t *testing.T, wishes ...*wishlistv1alpha1.Wish) *Server {
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

	return NewServer(fakeClient, "default", 30, 10)
}

func TestServer_HandleIndex(t *testing.T) {
	t.Parallel()

	wish := &wishlistv1alpha1.Wish{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-wish",
			Namespace: "default",
		},
		Spec: wishlistv1alpha1.WishSpec{
			Title:    "Test Gift",
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
	assert.Contains(t, rec.Body.String(), "Test Gift")
}

func TestServer_HandleWishes(t *testing.T) {
	t.Parallel()

	wish := &wishlistv1alpha1.Wish{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-wish",
			Namespace: "default",
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
			Name:      "reserve-wish",
			Namespace: "default",
		},
		Spec: wishlistv1alpha1.WishSpec{
			Title: "Reservable Gift",
		},
		Status: wishlistv1alpha1.WishStatus{
			Active:   true,
			Reserved: false,
		},
	}

	srv := newTestServer(t, wish)
	handler := srv.Handler()

	form := url.Values{}
	form.Set("weeks", "4")

	req := httptest.NewRequest(http.MethodPost, "/wishes/reserve-wish/reserve", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify wish is now reserved
	updatedWish := &wishlistv1alpha1.Wish{}
	err := srv.client.Get(context.Background(),
		client.ObjectKey{Name: "reserve-wish", Namespace: "default"},
		updatedWish)
	require.NoError(t, err)
	assert.True(t, updatedWish.Status.Reserved)
	assert.NotNil(t, updatedWish.Status.ReservedAt)
	assert.NotNil(t, updatedWish.Status.ReservationExpires)

	// Verify reservation is 4 weeks
	expectedExpiry := updatedWish.Status.ReservedAt.Add(4 * 7 * 24 * time.Hour)
	assert.WithinDuration(t, expectedExpiry, updatedWish.Status.ReservationExpires.Time, time.Second)
}

func TestServer_HandleReserve_AlreadyReserved(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	expires := metav1.NewTime(now.Add(7 * 24 * time.Hour))

	wish := &wishlistv1alpha1.Wish{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "reserved-wish",
			Namespace: "default",
		},
		Spec: wishlistv1alpha1.WishSpec{
			Title: "Already Reserved Gift",
		},
		Status: wishlistv1alpha1.WishStatus{
			Active:             true,
			Reserved:           true,
			ReservedAt:         &now,
			ReservationExpires: &expires,
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
					Namespace: "default",
				},
				Spec: wishlistv1alpha1.WishSpec{
					Title: "Test Gift",
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
