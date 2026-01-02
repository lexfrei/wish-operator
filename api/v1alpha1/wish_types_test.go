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

func TestWish_GetQuantity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		quantity int32
		expected int32
	}{
		{
			name:     "zero defaults to 1",
			quantity: 0,
			expected: 1,
		},
		{
			name:     "negative defaults to 1",
			quantity: -1,
			expected: 1,
		},
		{
			name:     "explicit 1",
			quantity: 1,
			expected: 1,
		},
		{
			name:     "explicit 5",
			quantity: 5,
			expected: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wish := &Wish{Spec: WishSpec{Quantity: tt.quantity}}
			assert.Equal(t, tt.expected, wish.GetQuantity())
		})
	}
}

func TestWish_TotalReserved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		reservations []Reservation
		expected     int32
	}{
		{
			name:         "no reservations",
			reservations: nil,
			expected:     0,
		},
		{
			name:         "empty slice",
			reservations: []Reservation{},
			expected:     0,
		},
		{
			name: "single reservation",
			reservations: []Reservation{
				{Quantity: 2},
			},
			expected: 2,
		},
		{
			name: "multiple reservations",
			reservations: []Reservation{
				{Quantity: 2},
				{Quantity: 3},
				{Quantity: 1},
			},
			expected: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wish := &Wish{Status: WishStatus{Reservations: tt.reservations}}
			assert.Equal(t, tt.expected, wish.TotalReserved())
		})
	}
}

func TestWish_AvailableQuantity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		quantity     int32
		reservations []Reservation
		expected     int32
	}{
		{
			name:         "all available (default quantity)",
			quantity:     0,
			reservations: nil,
			expected:     1,
		},
		{
			name:         "all available (explicit quantity)",
			quantity:     5,
			reservations: nil,
			expected:     5,
		},
		{
			name:     "partially reserved",
			quantity: 5,
			reservations: []Reservation{
				{Quantity: 2},
			},
			expected: 3,
		},
		{
			name:     "fully reserved",
			quantity: 3,
			reservations: []Reservation{
				{Quantity: 2},
				{Quantity: 1},
			},
			expected: 0,
		},
		{
			name:     "over-reserved (edge case)",
			quantity: 2,
			reservations: []Reservation{
				{Quantity: 3},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wish := &Wish{
				Spec:   WishSpec{Quantity: tt.quantity},
				Status: WishStatus{Reservations: tt.reservations},
			}
			assert.Equal(t, tt.expected, wish.AvailableQuantity())
		})
	}
}

func TestWish_ActiveReservations(t *testing.T) {
	t.Parallel()

	future := metav1.NewTime(time.Now().Add(time.Hour))
	past := metav1.NewTime(time.Now().Add(-time.Hour))

	tests := []struct {
		name         string
		reservations []Reservation
		expectedLen  int
	}{
		{
			name:         "no reservations",
			reservations: nil,
			expectedLen:  0,
		},
		{
			name: "all active",
			reservations: []Reservation{
				{Quantity: 1, ExpiresAt: future},
				{Quantity: 2, ExpiresAt: future},
			},
			expectedLen: 2,
		},
		{
			name: "all expired",
			reservations: []Reservation{
				{Quantity: 1, ExpiresAt: past},
				{Quantity: 2, ExpiresAt: past},
			},
			expectedLen: 0,
		},
		{
			name: "mixed",
			reservations: []Reservation{
				{Quantity: 1, ExpiresAt: future},
				{Quantity: 2, ExpiresAt: past},
				{Quantity: 3, ExpiresAt: future},
			},
			expectedLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wish := &Wish{Status: WishStatus{Reservations: tt.reservations}}
			active := wish.ActiveReservations()
			assert.Len(t, active, tt.expectedLen)
		})
	}
}

func TestWish_IsFullyReserved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		quantity     int32
		reservations []Reservation
		expected     bool
	}{
		{
			name:         "not reserved at all",
			quantity:     3,
			reservations: nil,
			expected:     false,
		},
		{
			name:     "partially reserved",
			quantity: 3,
			reservations: []Reservation{
				{Quantity: 1},
			},
			expected: false,
		},
		{
			name:     "exactly fully reserved",
			quantity: 3,
			reservations: []Reservation{
				{Quantity: 2},
				{Quantity: 1},
			},
			expected: true,
		},
		{
			name:     "default quantity fully reserved",
			quantity: 0,
			reservations: []Reservation{
				{Quantity: 1},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wish := &Wish{
				Spec:   WishSpec{Quantity: tt.quantity},
				Status: WishStatus{Reservations: tt.reservations},
			}
			assert.Equal(t, tt.expected, wish.IsFullyReserved())
		})
	}
}

func TestWish_NextReservationExpiry(t *testing.T) {
	t.Parallel()

	now := time.Now()
	in1Hour := metav1.NewTime(now.Add(time.Hour))
	in2Hours := metav1.NewTime(now.Add(2 * time.Hour))
	in30Min := metav1.NewTime(now.Add(30 * time.Minute))

	tests := []struct {
		name         string
		reservations []Reservation
		expectNil    bool
		expectedTime *metav1.Time
	}{
		{
			name:         "no reservations",
			reservations: nil,
			expectNil:    true,
		},
		{
			name: "single reservation",
			reservations: []Reservation{
				{Quantity: 1, ExpiresAt: in1Hour},
			},
			expectNil:    false,
			expectedTime: &in1Hour,
		},
		{
			name: "multiple reservations - finds earliest",
			reservations: []Reservation{
				{Quantity: 1, ExpiresAt: in2Hours},
				{Quantity: 2, ExpiresAt: in30Min},
				{Quantity: 1, ExpiresAt: in1Hour},
			},
			expectNil:    false,
			expectedTime: &in30Min,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wish := &Wish{Status: WishStatus{Reservations: tt.reservations}}
			result := wish.NextReservationExpiry()
			if tt.expectNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.expectedTime.Time, result.Time)
			}
		})
	}
}
