// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestCatalogsCoverEnglish pins every catalog against the English one. A key
// added for one language and forgotten in another makes T fall through to
// returning the bare key, which reaches the page as a literal like
// "err_get_wish", so the gap has to fail here instead.
//
// English is the reference because T falls back to it, and coverage is
// one-directional on purpose: a language may carry extra keys English has no
// use for. Russian needs weeks_few and weeks_many for its three plural forms,
// where English gets by with two.
func TestCatalogsCoverEnglish(t *testing.T) {
	t.Parallel()

	reference, ok := messages[LangEN]
	if !ok {
		t.Fatalf("no catalog for reference language %q", LangEN)
	}

	for lang, catalog := range messages {
		for key := range reference {
			if _, found := catalog[key]; !found {
				t.Errorf("catalog %q is missing key %q", lang, key)
			}
		}

		for key, value := range catalog {
			if value == "" {
				t.Errorf("catalog %q has an empty translation for %q", lang, key)
			}
		}
	}
}

// TestEveryKeyResolves checks that T returns a translation rather than echoing
// the key back, for every key in every catalog.
func TestEveryKeyResolves(t *testing.T) {
	t.Parallel()

	for lang, catalog := range messages {
		for key := range catalog {
			if got := T(lang, key); got == key {
				t.Errorf("T(%q, %q) returned the key itself", lang, key)
			}
		}
	}
}

// callerDirs are the trees that ask for translations, relative to this package.
// Templates are covered through their generated _templ.go, which is committed.
//
// This package contributes i18n.go alone rather than the whole directory. Naming
// the callers is safer than excluding the catalogs by filename: splitting them
// into messages_ru.go would put every `keyFoo:` entry back in scope as a
// self-referential caller and quietly pass the orphan check forever after.
//
//nolint:gochecknoglobals // test fixture, kept next to the test that reads it
var (
	callerDirs  = []string{"../templates", "../web"}
	callerFiles = []string{"i18n.go"}
)

// TestNoOrphanKeys fails on a key nothing asks for. Catalog coverage alone
// cannot see this: a key translated into all three languages and referenced by
// nobody satisfies every other check in this file, which is how the strings of
// the single-reservation UI outlived the code that rendered them.
//
// A key reaches the page one of two ways: templates and handlers pass the
// string literal to T, while this package's own helpers go through the key*
// constant. So a key counts as used if either spelling appears in the syntax
// tree of a non-test source.
//
// Matching runs over parsed sources rather than raw bytes on purpose. A
// substring search counts a key mentioned in a comment — including the comments
// in this very file — as a live caller, which makes the check pass for keys
// nothing renders. Test sources are skipped for the same reason: an assertion
// naming a key is not a caller of it.
func TestNoOrphanKeys(t *testing.T) {
	t.Parallel()

	symbols := collectCallerSymbols(t)

	for key := range allKeys() {
		if symbols.literals[key] || symbols.idents[keyConstantName(key)] {
			continue
		}

		t.Errorf("key %q has no caller: neither the literal nor %s appears outside the catalogs and tests",
			key, keyConstantName(key))
	}
}

// TestNoMissingKeys is the other direction, and the one a deletion can actually
// break: a key somebody passes to T that no catalog answers. T returns the key
// unchanged in that case, so the page renders the string "buy_label" where the
// word "Buy:" belongs — no panic, no error, nothing red until someone looks at
// the page in that language.
func TestNoMissingKeys(t *testing.T) {
	t.Parallel()

	symbols := collectCallerSymbols(t)
	known := allKeys()

	if len(symbols.requested) == 0 {
		t.Fatal("found no T(lang, key) calls to check; the scan is looking in the wrong place")
	}

	for key, origin := range symbols.requested {
		if _, found := known[key]; !found {
			t.Errorf("%s asks T for key %q, which no catalog defines", origin, key)
		}
	}
}

// allKeys unions every catalog, so an orphan that exists only in the ru or zh
// map is caught as well.
func allKeys() map[string]struct{} {
	keys := make(map[string]struct{})

	for _, catalog := range messages {
		for key := range catalog {
			keys[key] = struct{}{}
		}
	}

	return keys
}

// keyConstantName maps a catalog key to the constant this package declares for
// it: err_get_wish -> keyErrGetWish.
func keyConstantName(key string) string {
	var b strings.Builder

	b.WriteString("key")

	for word := range strings.SplitSeq(key, "_") {
		if word == "" {
			continue
		}

		b.WriteString(strings.ToUpper(word[:1]))
		b.WriteString(word[1:])
	}

	return b.String()
}

// TestCollectSymbolsIgnoresComments pins the property TestNoOrphanKeys rests
// on: a key named only in a comment is not a caller. A raw substring search
// over the same source reports it as used, which is what made the first
// version of this guard pass for keys nothing renders.
func TestCollectSymbolsIgnoresComments(t *testing.T) {
	t.Parallel()

	const src = `package sample

// keyOnlyMentioned renders "orphan_probe" somewhere, allegedly.
func sample() string { return T(LangEN, "live_key") }
`

	symbols := callerSymbols{
		literals:  make(map[string]bool),
		idents:    make(map[string]bool),
		requested: make(map[string]string),
	}
	collectSymbols(t, src, &symbols, "sample.go")

	if symbols.literals["orphan_probe"] {
		t.Error(`"orphan_probe" appears only in a comment but was collected as a literal`)
	}

	if !symbols.literals["live_key"] {
		t.Error(`"live_key" is passed to T but was not collected`)
	}

	if symbols.idents["keyOnlyMentioned"] {
		t.Error("keyOnlyMentioned appears only in a comment but was collected as an identifier")
	}

	if symbols.requested["live_key"] != "sample.go" {
		t.Errorf(`"live_key" should be recorded as requested from sample.go, got %q`, symbols.requested["live_key"])
	}

	if _, found := symbols.requested["orphan_probe"]; found {
		t.Error(`"orphan_probe" is only named in a comment and must not count as requested`)
	}
}

// callerSymbols is what the callers of this package spell out: the string
// literals and identifiers they mention, and the keys they actually pass to T.
type callerSymbols struct {
	literals  map[string]bool
	idents    map[string]bool
	requested map[string]string // key -> where it was requested from
}

// collectCallerSymbols parses every non-test Go source that asks for translations.
func collectCallerSymbols(t *testing.T) callerSymbols {
	t.Helper()

	symbols := callerSymbols{
		literals:  make(map[string]bool),
		idents:    make(map[string]bool),
		requested: make(map[string]string),
	}

	paths := append([]string{}, callerFiles...)

	for _, dir := range callerDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("reading %s: %v", dir, err)
		}

		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}

			paths = append(paths, filepath.Join(dir, name))
		}
	}

	if len(paths) == 0 {
		t.Fatal("found no sources to scan for translation keys")
	}

	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}

		collectSymbols(t, string(body), &symbols, path)
	}

	return symbols
}

// collectSymbols walks the syntax tree of src, recording string literal values,
// identifier names, and the keys handed to T. Comments are absent from the tree,
// which is the point.
func collectSymbols(t *testing.T, src string, symbols *callerSymbols, origin string) {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v", origin, err)
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BasicLit:
			if node.Kind == token.STRING {
				value, err := strconv.Unquote(node.Value)
				if err == nil {
					symbols.literals[value] = true
				}
			}
		case *ast.Ident:
			symbols.idents[node.Name] = true
		case *ast.CallExpr:
			if key, ok := translationKeyOf(node); ok {
				symbols.requested[key] = origin
			}
		}

		return true
	})
}

// translationKeyOf returns the key a T(lang, key) call asks for, when the key is
// written as a literal. Calls that compute the key are invisible here, which is
// the price of not running the program.
func translationKeyOf(call *ast.CallExpr) (string, bool) {
	var name string

	switch fun := call.Fun.(type) {
	case *ast.Ident: // T(...) from inside this package
		name = fun.Name
	case *ast.SelectorExpr: // i18n.T(...) from outside
		name = fun.Sel.Name
	default:
		return "", false
	}

	const wantArgs = 2
	if name != "T" || len(call.Args) != wantArgs {
		return "", false
	}

	lit, ok := call.Args[1].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}

	key, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}

	return key, true
}

// languageSpecificKeys are plural forms that exist only where the grammar
// needs them. Weeks asks for these by language rather than by the caller's
// locale, so they have no English counterpart and never will.
func languageSpecificKeys() map[string]bool {
	return map[string]bool{
		keyWeeksFew:  true,
		keyWeeksMany: true,
	}
}

// TestMessagesCoverEveryLanguage pins two things TestCatalogsCoverEnglish does
// not: that a format key keeps the same number of %d and %s verbs across
// languages, since a translation that drops one renders %!s(MISSING) at
// runtime; and that no catalog carries a key English lacks, which is usually a
// typo, because every lookup starts from an English key. The one legitimate
// exception is grammar English does not have, listed in languageSpecificKeys.
func TestMessagesCoverEveryLanguage(t *testing.T) {
	t.Parallel()

	verbs := []string{"%d", "%s"}
	pluralOnly := languageSpecificKeys()

	for _, lang := range supportedLangs {
		if lang == LangEN {
			continue
		}

		for key, english := range messages[LangEN] {
			translated, ok := messages[lang][key]
			if !ok {
				t.Errorf("catalog %q is missing key %q", lang, key)

				continue
			}

			for _, verb := range verbs {
				if strings.Count(english, verb) != strings.Count(translated, verb) {
					t.Errorf("%s translation of %q has a different number of %s verbs", lang, key, verb)
				}
			}
		}

		for key := range messages[lang] {
			if pluralOnly[key] {
				continue
			}

			if _, ok := messages[LangEN][key]; !ok {
				t.Errorf("%s has key %q that English does not", lang, key)
			}
		}
	}
}
