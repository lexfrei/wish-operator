// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package v1alpha1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestWishSpec_Fields(t *testing.T) {
	t.Parallel()

	spec := WishSpec{
		Title:        "Test Gift",
		ImageURL:     "https://example.com/image.jpg",
		OfficialURL:  "https://example.com/product",
		PurchaseURLs: []string{"https://shop1.com/buy", "https://shop2.com/buy"},
		MSRP:         "₽ 19900",
		Tags:         []string{"electronics", "gadgets"},
		ContextTags:  []string{"birthday", "christmas"},
		Description:  "I really want this because...",
		Priority:     5,
		TTL:          &metav1.Duration{Duration: 30 * 24 * time.Hour},
	}

	assert.Equal(t, "Test Gift", spec.Title)
	assert.Equal(t, "https://example.com/image.jpg", spec.ImageURL)
	assert.Equal(t, "https://example.com/product", spec.OfficialURL)
	assert.Equal(t, []string{"https://shop1.com/buy", "https://shop2.com/buy"}, spec.PurchaseURLs)
	assert.Equal(t, "₽ 19900", spec.MSRP)
	assert.Equal(t, []string{"electronics", "gadgets"}, spec.Tags)
	assert.Equal(t, []string{"birthday", "christmas"}, spec.ContextTags)
	assert.Equal(t, "I really want this because...", spec.Description)
	assert.Equal(t, int32(5), spec.Priority)
	require.NotNil(t, spec.TTL)
	assert.Equal(t, 30*24*time.Hour, spec.TTL.Duration)
}

func TestWishStatus_Fields(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	expires := metav1.NewTime(now.Add(7 * 24 * time.Hour))

	status := WishStatus{
		Reserved:           true,
		ReservedAt:         &now,
		ReservationExpires: &expires,
		Active:             true,
	}

	assert.True(t, status.Reserved)
	require.NotNil(t, status.ReservedAt)
	assert.Equal(t, now.Time, status.ReservedAt.Time)
	require.NotNil(t, status.ReservationExpires)
	assert.Equal(t, expires.Time, status.ReservationExpires.Time)
	assert.True(t, status.Active)
}

func TestWish_DefaultValues(t *testing.T) {
	t.Parallel()

	wish := &Wish{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-wish",
			Namespace: "default",
		},
		Spec: WishSpec{
			Title: "Minimal Wish",
		},
	}

	// Optional fields should be empty/zero by default
	assert.Empty(t, wish.Spec.ImageURL)
	assert.Empty(t, wish.Spec.OfficialURL)
	assert.Empty(t, wish.Spec.PurchaseURLs)
	assert.Empty(t, wish.Spec.MSRP)
	assert.Empty(t, wish.Spec.Tags)
	assert.Empty(t, wish.Spec.ContextTags)
	assert.Empty(t, wish.Spec.Description)
	assert.Equal(t, int32(0), wish.Spec.Priority)
	assert.Nil(t, wish.Spec.TTL)

	// Status should be empty by default
	assert.False(t, wish.Status.Reserved)
	assert.Nil(t, wish.Status.ReservedAt)
	assert.Nil(t, wish.Status.ReservationExpires)
	assert.False(t, wish.Status.Active)
}

func TestWish_IsReservationExpired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		status   WishStatus
		expected bool
	}{
		{
			name:     "not reserved",
			status:   WishStatus{Reserved: false},
			expected: false,
		},
		{
			name: "reserved but no expiration set",
			status: WishStatus{
				Reserved: true,
			},
			expected: false,
		},
		{
			name: "reservation not expired",
			status: WishStatus{
				Reserved:           true,
				ReservationExpires: timePtr(metav1.NewTime(time.Now().Add(time.Hour))),
			},
			expected: false,
		},
		{
			name: "reservation expired",
			status: WishStatus{
				Reserved:           true,
				ReservationExpires: timePtr(metav1.NewTime(time.Now().Add(-time.Hour))),
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wish := &Wish{Status: tt.status}
			assert.Equal(t, tt.expected, wish.IsReservationExpired())
		})
	}
}

func TestWish_IsExpired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		wish     Wish
		expected bool
	}{
		{
			name: "no TTL set - never expires",
			wish: Wish{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(time.Now().Add(-365 * 24 * time.Hour)),
				},
				Spec: WishSpec{TTL: nil},
			},
			expected: false,
		},
		{
			name: "within TTL",
			wish: Wish{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)),
				},
				Spec: WishSpec{TTL: &metav1.Duration{Duration: 24 * time.Hour}},
			},
			expected: false,
		},
		{
			name: "TTL expired",
			wish: Wish{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(time.Now().Add(-48 * time.Hour)),
				},
				Spec: WishSpec{TTL: &metav1.Duration{Duration: 24 * time.Hour}},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.wish.IsExpired())
		})
	}
}

func timePtr(t metav1.Time) *metav1.Time {
	return &t
}
