// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package controller

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	wishlistv1alpha1 "github.com/lexfrei/wish-operator/api/v1alpha1"
)

// WishReconciler reconciles a Wish object
type WishReconciler struct {
	client.Client

	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=wishlist.k8s.lex.la,resources=wishes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=wishlist.k8s.lex.la,resources=wishes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=wishlist.k8s.lex.la,resources=wishes/finalizers,verbs=update

// Reconcile handles the reconciliation of Wish resources.
// It manages TTL expiration and reservation cleanup.
//
//nolint:gocognit // Standard reconcile pattern with migration and cleanup logic
func (r *WishReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	wish := &wishlistv1alpha1.Wish{}
	if err := r.Get(ctx, req.NamespacedName, wish); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	statusChanged := false
	var requeueAfter time.Duration

	// Check and update Active status based on TTL
	isActive := !wish.IsExpired()
	if wish.Status.Active != isActive {
		wish.Status.Active = isActive
		statusChanged = true
		log.Info("Updated Active status", "active", isActive)
	}

	// Schedule requeue for TTL expiration if active and TTL is set
	if isActive && wish.Spec.TTL != nil {
		expiresAt := wish.CreationTimestamp.Add(wish.Spec.TTL.Duration)
		ttlRemaining := time.Until(expiresAt)
		if ttlRemaining > 0 {
			if requeueAfter == 0 || ttlRemaining < requeueAfter {
				requeueAfter = ttlRemaining
			}
		}
	}

	// Migration: convert legacy Reserved format to new Reservations slice
	//nolint:staticcheck // Intentional use of deprecated fields for migration
	if wish.Status.Reserved && len(wish.Status.Reservations) == 0 {
		//nolint:staticcheck // Intentional use of deprecated fields for migration
		if wish.Status.ReservedAt != nil && wish.Status.ReservationExpires != nil {
			wish.Status.Reservations = []wishlistv1alpha1.Reservation{{
				Quantity: 1,
				//nolint:staticcheck // Intentional use of deprecated fields for migration
				CreatedAt: *wish.Status.ReservedAt,
				//nolint:staticcheck // Intentional use of deprecated fields for migration
				ExpiresAt: *wish.Status.ReservationExpires,
			}}
			log.Info("Migrated legacy reservation to new format")
		}

		//nolint:staticcheck // Intentional use of deprecated fields for migration
		wish.Status.Reserved = false
		//nolint:staticcheck // Intentional use of deprecated fields for migration
		wish.Status.ReservedAt = nil
		//nolint:staticcheck // Intentional use of deprecated fields for migration
		wish.Status.ReservationExpires = nil
		statusChanged = true
	}

	// Clean up expired reservations from the slice
	now := time.Now()
	activeReservations := make([]wishlistv1alpha1.Reservation, 0, len(wish.Status.Reservations))

	for _, res := range wish.Status.Reservations {
		if res.ExpiresAt.After(now) {
			activeReservations = append(activeReservations, res)
		} else {
			statusChanged = true
			log.Info("Removed expired reservation", "quantity", res.Quantity, "expiredAt", res.ExpiresAt)
		}
	}

	if len(activeReservations) != len(wish.Status.Reservations) {
		wish.Status.Reservations = activeReservations
	}

	// Schedule requeue for next reservation expiry
	if next := wish.NextReservationExpiry(); next != nil {
		remaining := time.Until(next.Time)
		if remaining > 0 {
			if requeueAfter == 0 || remaining < requeueAfter {
				requeueAfter = remaining
			}
		}
	}

	if statusChanged {
		if err := r.Status().Update(ctx, wish); err != nil {
			log.Error(err, "Failed to update Wish status")

			return ctrl.Result{}, err
		}
	}

	if requeueAfter > 0 {
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *WishReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&wishlistv1alpha1.Wish{}).
		Named("wish").
		Complete(r)
}
