// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package v1alpha1

import (
	"math"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// Limits that keep a wish updatable. They live here rather than in the web
// package because the page has to honour the same numbers the server enforces:
// a form that offers what the server refuses is a bug in both directions.
const (
	// MaxReservations is how many live reservations one wish may carry.
	MaxReservations = 100

	// MaxQuantityPerRequest is how many items one reservation may claim on a
	// wish with unlimited quantity, where nothing else bounds the request.
	MaxQuantityPerRequest = 100
)

// Reservation represents a single reservation of one or more items.
type Reservation struct {
	// Quantity is the number of items reserved in this reservation.
	// +kubebuilder:validation:Minimum=1
	Quantity int32 `json:"quantity"`

	// CreatedAt is when this reservation was made.
	CreatedAt metav1.Time `json:"createdAt"`

	// ExpiresAt is when this reservation will expire.
	ExpiresAt metav1.Time `json:"expiresAt"`
}

// WishSpec defines the desired state of Wish.
type WishSpec struct {
	// Title is the name of the desired item.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Title string `json:"title"`

	// ImageURL is the URL to the product image.
	// +optional
	ImageURL string `json:"imageURL,omitempty"`

	// OfficialURL is the link to the official product page.
	// +optional
	OfficialURL string `json:"officialURL,omitempty"`

	// PurchaseURLs is a list of links where the item can be purchased.
	// +optional
	PurchaseURLs []string `json:"purchaseURLs,omitempty"`

	// MSRP is the price display string (e.g., "₽ 19900").
	// +optional
	MSRP string `json:"msrp,omitempty"`

	// Tags are category labels for the wish.
	// +optional
	Tags []string `json:"tags,omitempty"`

	// ContextTags describe occasions (e.g., "birthday", "christmas").
	// +optional
	ContextTags []string `json:"contextTags,omitempty"`

	// Description explains why the user wants this item.
	// +optional
	Description string `json:"description,omitempty"`

	// Priority indicates importance, displayed as that many stars.
	// 0 is the unset value and renders no stars at all.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=5
	// +optional
	Priority int32 `json:"priority,omitempty"`

	// TTL defines how long the wish stays active.
	// +optional
	TTL *metav1.Duration `json:"ttl,omitempty"`

	// Quantity is the total number of items available for this wish.
	// Defaults to 1 if not specified (backwards compatible).
	// Set to 0 for unlimited quantity, meaning no stock to run out.
	//
	// Every wish, whatever its quantity, holds at most 100 live reservations
	// at a time and refuses further ones until some expire. A single
	// reservation is bounded by whatever stock is left, so a wish with 500
	// items can be reserved 500 at once; on an unlimited wish, where nothing
	// is left to run out, one reservation claims at most 100 items.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=1
	// +optional
	Quantity int32 `json:"quantity,omitempty"`
}

// WishStatus defines the observed state of Wish.
type WishStatus struct {
	// Reservations is a list of active reservations for this wish.
	// +optional
	Reservations []Reservation `json:"reservations,omitempty"`

	// Active indicates if the wish is within its TTL.
	// +optional
	Active bool `json:"active,omitempty"`

	// Conditions represent the current state of the Wish resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Wish is the Schema for the wishes API
type Wish struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Wish
	// +required
	Spec WishSpec `json:"spec"`

	// status defines the observed state of Wish
	// +optional
	Status WishStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// WishList contains a list of Wish
type WishList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`

	Items []Wish `json:"items"`
}

// IsUnlimited returns true if the wish has unlimited quantity (quantity == 0).
func (w *Wish) IsUnlimited() bool {
	return w.Spec.Quantity == 0
}

// GetQuantity returns the total quantity.
// In real K8s with kubebuilder, default=1 ensures unset becomes 1.
// Explicit 0 means unlimited. Negative values fallback to 1 (shouldn't happen due to validation).
func (w *Wish) GetQuantity() int32 {
	if w.Spec.Quantity < 0 {
		return 1
	}

	return w.Spec.Quantity
}

// TotalReserved returns the sum of quantities across reservations that have not expired.
// Expired entries are ignored so they stop holding items once their window closes,
// even before the reconciler prunes them from the status.
//
// The expiry check is inlined rather than reusing ActiveReservations because the card
// template calls AvailableQuantity in a loop condition, so this runs once per iteration.
func (w *Wish) TotalReserved() int32 {
	now := time.Now()

	var total int32

	for _, r := range w.Status.Reservations {
		if r.ExpiresAt.After(now) {
			total += r.Quantity
		}
	}

	return total
}

// AvailableQuantity returns how many items are available for reservation.
// For unlimited wishes (quantity == 0), returns math.MaxInt32.
func (w *Wish) AvailableQuantity() int32 {
	if w.IsUnlimited() {
		return math.MaxInt32
	}

	available := w.GetQuantity() - w.TotalReserved()
	if available < 0 {
		return 0
	}

	return available
}

// ActiveReservations returns reservations that have not yet expired.
func (w *Wish) ActiveReservations() []Reservation {
	now := time.Now()

	var active []Reservation

	for _, r := range w.Status.Reservations {
		if r.ExpiresAt.After(now) {
			active = append(active, r)
		}
	}

	return active
}

// AtReservationLimit reports whether the wish already carries as many live
// reservations as it can hold. Reserving is anonymous, so without a ceiling a
// request loop grows status.reservations until the object no longer fits what
// the API server accepts, after which nothing can update the wish again --
// including the controller pass that would have pruned the list.
func (w *Wish) AtReservationLimit() bool {
	return len(w.ActiveReservations()) >= MaxReservations
}

// MaxReservableQuantity returns the largest quantity one reservation may claim
// right now. Zero means the wish cannot take a reservation at all, so the same
// call decides whether a reserve form is offered and how far it may go.
func (w *Wish) MaxReservableQuantity() int32 {
	if w.AtReservationLimit() {
		return 0
	}

	// An unlimited wish has no stock to run out, so the per-request limit is
	// the only thing bounding it. A limited one is bounded by what is left.
	if w.IsUnlimited() {
		return MaxQuantityPerRequest
	}

	return w.AvailableQuantity()
}

// IsFullyReserved returns true if all items are reserved.
// Unlimited wishes (quantity == 0) are never fully reserved.
func (w *Wish) IsFullyReserved() bool {
	if w.IsUnlimited() {
		return false
	}

	return w.AvailableQuantity() == 0
}

// NextReservationExpiry returns the earliest expiration time among reservations
// that have not expired yet, which is when the wish next needs reconciling.
// Returns nil if there are none. Entries already past their expiry are skipped:
// their time lies in the past and would schedule a requeue that never fires.
func (w *Wish) NextReservationExpiry() *metav1.Time {
	active := w.ActiveReservations()

	var earliest *metav1.Time

	for i := range active {
		r := &active[i]
		if earliest == nil || r.ExpiresAt.Time.Before(earliest.Time) {
			earliest = &r.ExpiresAt
		}
	}

	return earliest
}

// IsExpired checks if the wish has exceeded its TTL.
func (w *Wish) IsExpired() bool {
	if w.Spec.TTL == nil {
		return false
	}

	expirationTime := w.CreationTimestamp.Add(w.Spec.TTL.Duration)

	return time.Now().After(expirationTime)
}

func init() {
	objectTypes = append(objectTypes, &Wish{}, &WishList{})
}
