// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package v1alpha1

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

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
}

// WishStatus defines the observed state of Wish.
type WishStatus struct {
	// Reserved indicates if someone has reserved this wish.
	// +optional
	Reserved bool `json:"reserved,omitempty"`

	// ReservedAt is the timestamp when the wish was reserved.
	// +optional
	ReservedAt *metav1.Time `json:"reservedAt,omitempty"`

	// ReservationExpires is when the reservation will expire (1-8 weeks from reservedAt).
	// +optional
	ReservationExpires *metav1.Time `json:"reservationExpires,omitempty"`

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

// IsReservationExpired checks if the reservation has expired.
func (w *Wish) IsReservationExpired() bool {
	if !w.Status.Reserved {
		return false
	}

	if w.Status.ReservationExpires == nil {
		return false
	}

	return time.Now().After(w.Status.ReservationExpires.Time)
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
