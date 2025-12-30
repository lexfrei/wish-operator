// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package i18n

import (
	"net/http"
	"slices"
	"strings"
)

// Supported languages.
const (
	LangEN = "en"
	LangRU = "ru"
	LangZH = "zh"

	DefaultLang = LangEN
)

// supportedLangs contains all supported language codes.
var supportedLangs = []string{LangEN, LangRU, LangZH} //nolint:gochecknoglobals // immutable language list

// DetectLanguage determines the language from the request.
// Priority: 1) ?lang= parameter, 2) Accept-Language header, 3) default (en).
func DetectLanguage(r *http.Request) string {
	// Check query parameter first
	if lang := r.URL.Query().Get("lang"); lang != "" {
		if isSupported(lang) {
			return lang
		}
	}

	// Parse Accept-Language header
	acceptLang := r.Header.Get("Accept-Language")
	if acceptLang != "" {
		lang := parseAcceptLanguage(acceptLang)
		if lang != "" {
			return lang
		}
	}

	return DefaultLang
}

// parseAcceptLanguage extracts the best matching language from Accept-Language header.
func parseAcceptLanguage(header string) string {
	// Simple parsing: split by comma and check each language
	// Format: "en-US,en;q=0.9,ru;q=0.8,zh-CN;q=0.7"
	for part := range strings.SplitSeq(header, ",") {
		// Remove quality value
		lang, _, _ := strings.Cut(strings.TrimSpace(part), ";")

		// Get primary language tag (e.g., "en" from "en-US")
		primaryLang, _, _ := strings.Cut(lang, "-")

		if isSupported(primaryLang) {
			return primaryLang
		}
	}

	return ""
}

// isSupported checks if the language is in the supported list.
func isSupported(lang string) bool {
	return slices.Contains(supportedLangs, strings.ToLower(lang))
}

// T returns the translation for the given key in the specified language.
func T(lang, key string) string {
	if translations, ok := messages[lang]; ok {
		if msg, ok := translations[key]; ok {
			return msg
		}
	}

	// Fallback to English
	if translations, ok := messages[DefaultLang]; ok {
		if msg, ok := translations[key]; ok {
			return msg
		}
	}

	// Return key if translation not found
	return key
}

// Weeks returns the localized string for week duration.
func Weeks(lang string, n int) string {
	switch lang {
	case LangRU:
		return weeksRussian(n)
	case LangZH:
		return weeksChinese(n)
	default:
		return weeksEnglish(n)
	}
}

func weeksEnglish(n int) string {
	if n == 1 {
		return "1 week"
	}

	return T(LangEN, "weeks_format")
}

func weeksRussian(n int) string {
	// Russian pluralization rules
	if n == 1 {
		return "1 неделя"
	}

	lastTwo := n % 100
	lastOne := n % 10

	if lastTwo >= 11 && lastTwo <= 14 {
		return T(LangRU, "weeks_many") // недель
	}

	switch lastOne {
	case 1:
		return T(LangRU, "week_one") // неделя
	case 2, 3, 4: //nolint:mnd // Russian pluralization rules
		return T(LangRU, "weeks_few") // недели
	default:
		return T(LangRU, "weeks_many") // недель
	}
}

func weeksChinese(_ int) string {
	// Chinese doesn't have plural forms
	return T(LangZH, "weeks_format")
}
