// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package templates_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	wishlistv1alpha1 "github.com/lexfrei/wish-operator/api/v1alpha1"
	"github.com/lexfrei/wish-operator/internal/templates"
)

const testNamespace = "default"

// TestWishCardPriorityZeroRendersNoStars pins what the CRD description, the
// README table and CLAUDE.md all now promise: priority accepts 0, and 0 means
// unset rather than a zero-star rating. The only thing implementing that is the
// `if wish.Spec.Priority > 0` guard in the template.
func TestWishCardPriorityZeroRendersNoStars(t *testing.T) {
	t.Parallel()

	render := func(t *testing.T, priority int32) string {
		t.Helper()

		wish := &wishlistv1alpha1.Wish{
			ObjectMeta: metav1.ObjectMeta{Name: "priority-wish", Namespace: testNamespace},
			Spec: wishlistv1alpha1.WishSpec{
				Title:    "Priority Gift",
				Quantity: 1,
				Priority: priority,
			},
			Status: wishlistv1alpha1.WishStatus{Active: true},
		}

		var out strings.Builder

		require.NoError(t, templates.WishCard(wish, "en").Render(context.Background(), &out))

		return out.String()
	}

	assert.NotContains(t, render(t, 0), `class="stars"`, "priority 0 is unset and renders no stars")
	assert.Contains(t, render(t, 3), `class="stars"`, "a set priority still renders stars")
}

// TestWishCardIgnoresExpiredReservation renders the case the availability rule
// exists for: one reservation lapsed, one still live, against a quantity of two.
// The card has to offer the freed item and list only the live reservation.
func TestWishCardIgnoresExpiredReservation(t *testing.T) {
	t.Parallel()

	past := metav1.NewTime(time.Now().Add(-time.Hour))
	future := metav1.NewTime(time.Now().Add(time.Hour))

	wish := &wishlistv1alpha1.Wish{
		ObjectMeta: metav1.ObjectMeta{Name: "card-wish", Namespace: testNamespace},
		Spec: wishlistv1alpha1.WishSpec{
			Title:    "Card Gift",
			Quantity: 2,
		},
		Status: wishlistv1alpha1.WishStatus{
			Active: true,
			Reservations: []wishlistv1alpha1.Reservation{
				{Quantity: 1, CreatedAt: past, ExpiresAt: past},
				{Quantity: 1, CreatedAt: past, ExpiresAt: future},
			},
		},
	}

	var out strings.Builder

	require.NoError(t, templates.WishCard(wish, "en").Render(context.Background(), &out))

	rendered := out.String()

	assert.Contains(t, rendered, "1/2", "the lapsed reservation should read as available again")
	assert.Equal(t, 1, strings.Count(rendered, "reserved until"),
		"only the live reservation belongs in the list")
}
