// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package v1alpha1_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	wishlistv1alpha1 "github.com/lexfrei/wish-operator/api/v1alpha1"
)

// manifests are the two copies of the CRD that ship to users. make manifests
// writes the first; the chart copy is updated by hand, so they drift silently.
var manifests = []string{
	filepath.Join("..", "..", "config", "crd", "bases", "wishlist.k8s.lex.la_wishes.yaml"),
	filepath.Join("..", "..", "charts", "wish-operator", "crds", "wishlist.k8s.lex.la_wishes.yaml"),
}

// TestCRDQuantityDescription pins what a cluster operator reads from kubectl
// explain wishes.spec.quantity. The description is generated from a Go doc
// comment, and the reader has no access to the package it came from: it has
// to carry the numbers themselves, not the names of the constants holding
// them.
func TestCRDQuantityDescription(t *testing.T) {
	t.Parallel()

	for _, path := range manifests {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			t.Parallel()

			raw, err := os.ReadFile(path)
			require.NoError(t, err)

			// The generator wraps long descriptions, so match on the words
			// rather than on a whole sentence.
			manifest := strings.Join(strings.Fields(string(raw)), " ")

			// The count limit applies to every wish, so it must not read as a
			// property of unlimited ones; the per-request limit is the opposite.
			assert.Contains(t, manifest,
				fmt.Sprintf("Every wish, whatever its quantity, holds at most %d live reservations",
					wishlistv1alpha1.MaxReservations))
			assert.Contains(t, manifest,
				fmt.Sprintf("on an unlimited wish, where nothing is left to run out, one reservation claims at most %d items",
					wishlistv1alpha1.MaxQuantityPerRequest))
			assert.NotContains(t, manifest, "MaxQuantityPerRequest")
			assert.NotContains(t, manifest, "MaxReservations")
		})
	}
}

// TestREADMEQuotesTheLimits pins the third copy of these numbers. The CRD
// description and the chart manifest are both checked above; the README says
// the same thing in prose and nothing else would notice it going stale.
func TestREADMEQuotesTheLimits(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	require.NoError(t, err)

	readme := string(raw)

	assert.Contains(t, readme,
		fmt.Sprintf("at most %d live reservations", wishlistv1alpha1.MaxReservations))
	assert.Contains(t, readme,
		fmt.Sprintf("at most %d items on a wish with unlimited quantity",
			wishlistv1alpha1.MaxQuantityPerRequest))
	assert.Contains(t, readme,
		fmt.Sprintf("one client taking all %d slots", wishlistv1alpha1.MaxReservations))
}

// TestCRDCopiesAreIdentical pins that the chart ships what the generator
// produced. make manifests only writes config/crd/bases, so the chart copy
// is one manual step away from being stale.
func TestCRDCopiesAreIdentical(t *testing.T) {
	t.Parallel()

	generated, err := os.ReadFile(manifests[0])
	require.NoError(t, err)

	shipped, err := os.ReadFile(manifests[1])
	require.NoError(t, err)

	assert.Equal(t, string(generated), string(shipped),
		"chart CRD is out of sync with config/crd/bases; copy it after make manifests")
}
