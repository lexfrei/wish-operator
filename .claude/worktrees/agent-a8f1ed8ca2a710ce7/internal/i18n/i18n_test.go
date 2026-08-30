// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package i18n_test

import (
	"testing"

	"github.com/lexfrei/wish-operator/internal/i18n"
)

// Repeated expected outputs, hoisted to satisfy goconst. The want* prefix
// keeps them distinct from the production translation constants.
const (
	wantEnWeeksPlural = "weeks"
	wantRuWeeksFew    = "недели"
	wantRuWeeksMany   = "недель"
)

// TestWeeks pins the per-language week pluralization and guards the i18n
// key constants against drift: Weeks resolves its words through T with the
// key* constants, so if a constant ever stops matching its translation-map
// key, T falls through to returning the bare key string and these
// assertions fail.
func TestWeeks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		lang string
		n    int
		want string
	}{
		{"en singular", i18n.LangEN, 1, "1 week"},
		{"en plural", i18n.LangEN, 3, wantEnWeeksPlural},
		{"unknown lang falls back to en", "fr", 3, wantEnWeeksPlural},
		{"ru singular", i18n.LangRU, 1, "1 неделя"},
		{"ru few 2", i18n.LangRU, 2, wantRuWeeksFew},
		{"ru few 23", i18n.LangRU, 23, wantRuWeeksFew},
		{"ru many 13", i18n.LangRU, 13, wantRuWeeksMany},
		{"ru many 5", i18n.LangRU, 5, wantRuWeeksMany},
		{"ru one 21", i18n.LangRU, 21, "неделя"},
		{"zh", i18n.LangZH, 5, "周"}, //nolint:gosmopolitan // Chinese week label is non-ASCII by design
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := i18n.Weeks(tc.lang, tc.n)
			if got != tc.want {
				t.Errorf("Weeks(%q, %d) = %q, want %q", tc.lang, tc.n, got, tc.want)
			}
		})
	}
}
