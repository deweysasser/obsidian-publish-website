// Package urls maps slug paths to site URLs for each output target.
package urls

import (
	"net/url"
	"strings"

	"github.com/deweysasser/obsidian-publish-website/internal/selector"
)

// Scheme maps slug paths to URLs.
type Scheme interface {
	PageURL(slug string) string
	AssetURL(slug string) string
	TagURL(tag string) string
}

// Basic is a prefix-based Scheme.
type Basic struct {
	PagePrefix  string // prepended to page slugs, ends in "/"
	AssetPrefix string
	TagPrefix   string
}

// HTML returns the scheme used by the html target.
func HTML(basePath string) Basic {
	return Basic{PagePrefix: basePath, AssetPrefix: basePath + "assets/", TagPrefix: basePath + "tags/"}
}

func (b Basic) PageURL(slug string) string {
	if slug == "" {
		return b.PagePrefix
	}
	return b.PagePrefix + Escape(slug) + "/"
}

func (b Basic) AssetURL(slug string) string { return b.AssetPrefix + Escape(slug) }

func (b Basic) TagURL(tag string) string {
	return b.TagPrefix + Escape(selector.Slugify(strings.TrimPrefix(tag, "#"))) + "/"
}

// Escape percent-encodes each segment of a slash path.
func Escape(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// BasePathOf returns the path component of a base URL, with leading and trailing "/".
func BasePathOf(baseURL string) string {
	p := baseURL
	if u, err := url.Parse(baseURL); err == nil {
		p = u.Path
	}
	p = strings.Trim(p, "/")
	if p == "" {
		return "/"
	}
	return "/" + p + "/"
}
