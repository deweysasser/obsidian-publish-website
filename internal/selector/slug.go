// Package selector decides which notes are published and assigns their URLs.
package selector

import (
	"path"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Slugify turns one path segment into a URL-safe slug: NFC, lowercase,
// letters and digits kept, every other run of characters becomes "-".
func Slugify(seg string) string {
	var b strings.Builder
	dash := false
	for _, r := range norm.NFC.String(strings.ToLower(seg)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if s == "" {
		return "untitled"
	}
	return s
}

// SlugPath slugifies every segment of a slash path.
func SlugPath(p string) string {
	if p == "" || p == "." {
		return ""
	}
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = Slugify(s)
	}
	return strings.Join(parts, "/")
}

// AssetSlug slugifies an attachment path, keeping a lowercase extension.
func AssetSlug(p string) string {
	ext := path.Ext(p)
	base := strings.TrimSuffix(p, ext)
	return SlugPath(base) + strings.ToLower(ext)
}
