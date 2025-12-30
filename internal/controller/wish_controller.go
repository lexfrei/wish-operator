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

	// Check and clear expired reservations
	if wish.IsReservationExpired() {
		wish.Status.Reserved = false
		wish.Status.ReservedAt = nil
		wish.Status.ReservationExpires = nil
		statusChanged = true
		log.Info("Cleared expired reservation")
	}

	// Schedule requeue for reservation expiration
	if wish.Status.Reserved && wish.Status.ReservationExpires != nil {
		reservationRemaining := time.Until(wish.Status.ReservationExpires.Time)
		if reservationRemaining > 0 {
			if requeueAfter == 0 || reservationRemaining < requeueAfter {
				requeueAfter = reservationRemaining
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
