// Package newsbuilder — locale helpers.
package newsbuilder

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/language"
)

// LocaleFromPath extracts the BCP 47 locale tag from a translation source path
// whose base name matches the pattern "entries.{locale}.html".
//
// For any path whose base name does not contain a locale segment (e.g. the
// canonical "entries.html"), it returns "en".
//
// Underscore separators in the filename (e.g. "entries.pt_BR.html",
// "entries.zh_TW.html") are converted to the hyphen form expected by BCP 47
// ("pt-BR", "zh-TW") before the tag is validated with golang.org/x/text/language.
// If the resulting raw tag cannot be parsed, the hyphenated raw string is
// returned as-is; the function never returns an empty string or panics.
//
// Examples:
//
//	LocaleFromPath("data/entries.html")               → "en"
//	LocaleFromPath("data/translations/entries.de.html") → "de"
//	LocaleFromPath("data/translations/entries.pt_BR.html") → "pt-BR"
//	LocaleFromPath("data/translations/entries.zh_TW.html") → "zh-TW"
func LocaleFromPath(path string) string {
	base := filepath.Base(path) // "entries.de.html"
	parts := strings.SplitN(base, ".", 3)
	// Must be exactly three dot-delimited segments: "entries", locale, "html".
	if len(parts) != 3 || parts[0] != "entries" || parts[2] != "html" {
		return "en"
	}
	// The locale segment is the middle part.
	raw := parts[1]
	if raw == "" {
		return "en"
	}
	// Filenames use underscores (e.g. "pt_BR") but BCP 47 uses hyphens.
	raw = strings.ReplaceAll(raw, "_", "-")
	tag, err := language.Parse(raw)
	if err != nil {
		// Return the hyphenated form even if the library cannot parse it so
		// that uncommon or future locale tags still appear in the output
		// rather than silently reverting to "en".
		return raw
	}
	return tag.String()
}

// DetectTranslationFiles returns the absolute paths of every
// "entries.{locale}.html" file found directly inside dir (non-recursive).
// Files whose base name does not match the three-segment pattern are silently
// skipped, so other HTML files co-located in the same directory are never
// mistaken for translation sources.  An empty or non-existent directory
// returns nil without error — callers treat that as "no translations available".
func DetectTranslationFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Must match exactly "entries.{locale}.html" — three dot segments,
		// first is "entries", last is "html".
		parts := strings.SplitN(name, ".", 3)
		if len(parts) != 3 || parts[0] != "entries" || parts[2] != "html" || parts[1] == "" {
			continue
		}
		paths = append(paths, filepath.Join(dir, name))
	}
	return paths
}
