// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package v1alpha1

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

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

	// Priority indicates importance (1-5, displayed as stars).
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=5
	// +optional
	Priority int32 `json:"priority,omitempty"`

	// TTL defines how long the wish stays active.
	// +optional
	TTL *metav1.Duration `json:"ttl,omitempty"`

	// Quantity is the total number of items available for this wish.
	// Defaults to 1 if not specified (backwards compatible).
	// +kubebuilder:validation:Minimum=1
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

	// --- DEPRECATED FIELDS (kept for backwards compatibility during migration) ---

	// Reserved indicates if someone has reserved this wish.
	//
	// Deprecated: Use Reservations slice instead.
	// +optional
	Reserved bool `json:"reserved,omitempty"`

	// ReservedAt is the timestamp when the wish was reserved.
	//
	// Deprecated: Use Reservations slice instead.
	// +optional
	ReservedAt *metav1.Time `json:"reservedAt,omitempty"`

	// ReservationExpires is when the reservation will expire (1-8 weeks from reservedAt).
	//
	// Deprecated: Use Reservations slice instead.
	// +optional
	ReservationExpires *metav1.Time `json:"reservationExpires,omitempty"`
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

// IsReservationExpired checks if the legacy reservation has expired.
//
// Deprecated: Use new Reservations slice instead.
func (w *Wish) IsReservationExpired() bool {
	if !w.Status.Reserved {
		return false
	}

	if w.Status.ReservationExpires == nil {
		return false
	}

	return time.Now().After(w.Status.ReservationExpires.Time)
}

// GetQuantity returns the total quantity, defaulting to 1 if not set.
func (w *Wish) GetQuantity() int32 {
	if w.Spec.Quantity <= 0 {
		return 1
	}

	return w.Spec.Quantity
}

// TotalReserved returns the sum of all reservation quantities.
func (w *Wish) TotalReserved() int32 {
	var total int32
	for _, r := range w.Status.Reservations {
		total += r.Quantity
	}

	return total
}

// AvailableQuantity returns how many items are available for reservation.
func (w *Wish) AvailableQuantity() int32 {
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

// IsFullyReserved returns true if all items are reserved.
func (w *Wish) IsFullyReserved() bool {
	return w.AvailableQuantity() == 0
}

// NextReservationExpiry returns the earliest expiration time among all reservations.
// Returns nil if there are no reservations.
func (w *Wish) NextReservationExpiry() *metav1.Time {
	if len(w.Status.Reservations) == 0 {
		return nil
	}

	var earliest *metav1.Time

	for i := range w.Status.Reservations {
		r := &w.Status.Reservations[i]
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
	SchemeBuilder.Register(&Wish{}, &WishList{})
}
