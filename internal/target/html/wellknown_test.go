package html

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/deweysasser/obsidian-publish-website/internal/render"
	"github.com/deweysasser/obsidian-publish-website/internal/target"
)

func TestSecurityTxt(t *testing.T) {
	out := t.TempDir()
	cfg := &config.Config{Output: out, Search: "none", Site: config.Site{Title: "S", BaseURL: "https://example.org/",
		SecurityTxt: &config.SecurityTxt{Contact: []string{"https://example.org/suggest/", "mailto:sec@example.org"},
			Policy: "https://example.org/policy/", PreferredLanguages: "en, es"}}}
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tg.now = func() time.Time { return time.Date(2026, 11, 13, 23, 59, 0, 0, time.FixedZone("x", -5*3600)) }
	if err := tg.Finalize(&target.Site{Config: cfg, Tree: navTree(), Tags: map[string][]*render.Page{}}); err != nil {
		t.Fatal(err)
	}
	// 2026-11-14 in UTC, plus the default 180 days
	want := "Contact: https://example.org/suggest/\nContact: mailto:sec@example.org\nExpires: 2027-05-13T00:00:00Z\n" +
		"Policy: https://example.org/policy/\nPreferred-Languages: en, es\nCanonical: https://example.org/.well-known/security.txt\n"
	for _, rel := range []string{".well-known/security.txt", "security.txt"} {
		if got := readOut(t, out, rel); got != want {
			t.Errorf("%s:\n%s\nwant:\n%s", rel, got, want)
		}
	}
}

func TestNoSecurityTxtUnlessConfigured(t *testing.T) {
	out, _ := finalizeFavicon(t, t.TempDir(), "")
	if _, err := os.Stat(filepath.Join(out, ".well-known")); err == nil {
		t.Error(".well-known published without site.security_txt")
	}
}

func redirectSite(t *testing.T, pages ...*render.Page) (out string, warnings []string) {
	t.Helper()
	return redirectSiteWith(t, nil, nil, pages...)
}

// redirectSiteWith builds a site with site.redirects and the given tag pages.
func redirectSiteWith(t *testing.T, redirects map[string]string, tags map[string][]*render.Page, pages ...*render.Page) (out string, warnings []string) {
	t.Helper()
	out = t.TempDir()
	cfg := &config.Config{Output: out, Root: "R", Search: "none", Site: config.Site{Title: "S", BaseURL: "https://example.org/",
		Suggest: &config.Suggest{Endpoint: "/api/suggest"}, Redirects: redirects, HiddenTags: []string{"secret"}}}
	cfg.ApplyDefaults()
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tg.Warn = func(w string) { warnings = append(warnings, w) }
	site := &target.Site{Config: cfg, Tree: &target.FolderNode{}, Pages: pages, Tags: tags}
	for _, p := range pages {
		if err := tg.WritePage(site, p); err != nil {
			t.Fatal(err)
		}
	}
	if err := tg.Finalize(site); err != nil {
		t.Fatal(err)
	}
	return out, warnings
}

func TestRedirectFrom(t *testing.T) {
	moved := &render.Page{Slug: "guides/setup", URL: "/guides/setup/", Title: "Setup & Use", SourcePath: "R/Guides/Setup.md",
		Frontmatter: map[string]any{"redirect_from": []any{"/old/guides/setup/", "/old.html", " ", 3}}}
	single := &render.Page{Slug: "b", URL: "/b/", Title: "B", SourcePath: "R/b.md", Frontmatter: map[string]any{"redirect_from": "/old-b"}}
	out, warnings := redirectSite(t, moved, single)
	if len(warnings) > 0 {
		t.Fatal(warnings)
	}
	stub := readOut(t, out, "old/guides/setup/index.html")
	for _, want := range []string{
		`<meta http-equiv="refresh" content="0; url=/guides/setup/">`,
		`<link rel="canonical" href="https://example.org/guides/setup/">`,
		`<meta name="robots" content="noindex">`,
		`<a href="/guides/setup/">Setup &amp; Use</a>`,
	} {
		if !strings.Contains(stub, want) {
			t.Errorf("stub lacks %s:\n%s", want, stub)
		}
	}
	if strings.Contains(stub, "data-pagefind-body") {
		t.Error("stub would be indexed by search")
	}
	if !strings.Contains(readOut(t, out, "old.html"), "url=/guides/setup/") {
		t.Error("/old.html: a path with an extension is written as that file")
	}
	if !strings.Contains(readOut(t, out, "old-b/index.html"), "url=/b/") {
		t.Error("a single string, without a trailing slash, is a directory")
	}
	if strings.Contains(readOut(t, out, "sitemap.xml"), "old/guides") {
		t.Error("stub listed in the sitemap")
	}
}

func TestRedirectFromNeverReplacesAFile(t *testing.T) {
	a := &render.Page{Slug: "a", URL: "/a/", Title: "A", SourcePath: "R/a.md", HTML: "<p>real a</p>"}
	b := &render.Page{Slug: "b", URL: "/b/", Title: "B", SourcePath: "R/b.md", Frontmatter: map[string]any{"redirect_from": []any{
		"/a/",            // a page
		"/favicon.ico",   // a generated file
		"/suggest/x/",    // reserved
		"/assets/x/",     // reserved
		"/.well-known/x", // reserved
		"/",              // the home page
		"/../escape/",    // not clean
		"https://x.io/y", // not a path
		"/q/?x=1",        // a query
		"relative/",      // no leading slash
		"/dup/",
	}}}
	c := &render.Page{Slug: "c", URL: "/c/", Title: "C", SourcePath: "R/c.md", Frontmatter: map[string]any{"redirect_from": "/dup/"}}
	out, warnings := redirectSite(t, a, b, c)
	if len(warnings) != 11 {
		t.Errorf("%d warnings, want 11:\n%s", len(warnings), strings.Join(warnings, "\n"))
	}
	for _, w := range warnings {
		if !strings.HasPrefix(w, "R/b.md: redirect_from ") && !strings.HasPrefix(w, "R/c.md: redirect_from ") {
			t.Errorf("warning doesn't name the note and key: %s", w)
		}
	}
	if !strings.Contains(readOut(t, out, "a/index.html"), "real a") {
		t.Error("page /a/ replaced by a redirect")
	}
	if !strings.Contains(readOut(t, out, "dup/index.html"), "url=/b/") {
		t.Error("the first note to claim /dup/ keeps it")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(out), "escape")); err == nil {
		t.Error("wrote outside the output directory")
	}
}

func TestSiteRedirects(t *testing.T) {
	post := &render.Page{Slug: "posts/hello", URL: "/posts/hello/", Title: "Hello", SourcePath: "R/Posts/Hello.md",
		Frontmatter: map[string]any{"redirect_from": "/claimed/"}}
	tags := map[string][]*render.Page{"#go": {post}}
	out, warnings := redirectSiteWith(t, map[string]string{
		"/categories/go/":     "#go",            // a tag page
		"/categories/golang/": "#Go",            // tags are case-insensitive
		"/hello.html":         "Posts/Hello.md", // a note
		"/claimed/":           "Posts/Hello.md", // a note's redirect_from wins
		"/categories/none/":   "#none",          // no note has the tag
		"/categories/s/":      "#secret",        // hidden tag: no page
		"/gone/":              "Posts/Gone.md",  // not a published note
		"/posts/hello/":       "#go",            // a real page is never replaced
	}, tags, post)
	if !strings.Contains(readOut(t, out, "categories/go/index.html"), `url=/tags/go/`) {
		t.Error("tag redirect missing")
	}
	if !strings.Contains(readOut(t, out, "categories/golang/index.html"), `url=/tags/go/`) {
		t.Error("tag case not folded")
	}
	if !strings.Contains(readOut(t, out, "hello.html"), `url=/posts/hello/`) {
		t.Error("note redirect missing")
	}
	want := []string{
		`site.redirects "/categories/none/": no published note has tag #none`,
		`site.redirects "/categories/s/": tag #secret is hidden, so it has no page`,
		`site.redirects "/claimed/": R/Posts/Hello.md: redirect_from already redirects there`,
		`site.redirects "/gone/": "R/Posts/Gone.md" is not a published note`,
		`site.redirects "/posts/hello/": a published page or file is already at that URL`,
	}
	if strings.Join(warnings, "\n") != strings.Join(want, "\n") {
		t.Errorf("warnings:\n%s\nwant:\n%s", strings.Join(warnings, "\n"), strings.Join(want, "\n"))
	}
	for _, f := range []string{"categories/none/index.html", "categories/s/index.html", "gone/index.html"} {
		if _, err := os.Stat(filepath.Join(out, f)); err == nil {
			t.Errorf("%s written for an unresolved target", f)
		}
	}
}
