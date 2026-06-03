// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

// Package v1alpha1 contains API Schema definitions for the wishlist v1alpha1 API group
// +kubebuilder:object:generate=true
// +groupName=wishlist.k8s.lex.la
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "wishlist.k8s.lex.la", Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

// objectTypes collects the API types registered by each type file's init();
// addKnownTypes installs the whole set when AddToScheme runs.
var objectTypes []runtime.Object

// addKnownTypes registers the collected types plus the GroupVersion metadata
// with the given scheme. This replaces controller-runtime's deprecated
// pkg/scheme.Builder so the api package depends only on apimachinery.
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion, objectTypes...)
	metav1.AddToGroupVersion(scheme, GroupVersion)

	return nil
}
