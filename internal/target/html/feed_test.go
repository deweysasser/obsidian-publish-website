package html

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/deweysasser/obsidian-publish-website/internal/render"
	"github.com/deweysasser/obsidian-publish-website/internal/target"
)

// feedSite builds a site with site.feed over R/Posts and returns its output.
func feedSite(t *testing.T, feed *config.Feed, pages ...*render.Page) (out string, warnings []string) {
	t.Helper()
	out = t.TempDir()
	cfg := &config.Config{Vault: t.TempDir(), Output: out, Root: "R", Search: "none",
		Site: config.Site{Title: "My Site", BaseURL: "https://example.org/", Feed: feed}}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tg.Warn = func(w string) { warnings = append(warnings, w) }
	var folderNote *render.Page
	for _, p := range pages {
		if p.SourcePath == "R/Posts/Posts.md" {
			folderNote = p
		}
	}
	tree := &target.FolderNode{Folders: []*target.FolderNode{{Name: "Posts", Folder: "R/Posts", Slug: "posts", URL: "/posts/", Page: folderNote}}}
	site := &target.Site{Config: cfg, Tree: tree, Pages: pages}
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

func post(name string, date any, extra map[string]any) *render.Page {
	slug := "posts/" + strings.ToLower(name)
	fm := map[string]any{"date": date}
	for k, v := range extra {
		fm[k] = v
	}
	return &render.Page{Slug: slug, URL: "/" + slug + "/", Title: name, SourcePath: "R/Posts/" + name + ".md", Frontmatter: fm,
		HTML: `<p>The body of ` + name + `, long enough to be a description. <a href="/posts/">All</a> <img src="/assets/x.png"> <a href="//cdn.example/y">y</a></p>`}
}

func TestFeed(t *testing.T) {
	ts := time.Date(2026, 11, 10, 15, 4, 0, 0, time.UTC)
	pages := []*render.Page{
		{Slug: "posts", URL: "/posts/", Title: "Posts", SourcePath: "R/Posts/Posts.md"}, // the folder note
		post("Old", "2026-01-02", map[string]any{"description": "Old & short <summary>."}),
		post("Newest", ts, nil),
		post("Tie-b", "2026-06-01", nil),
		post("Tie-a", "2026-06-01", nil),
		post("Undated", nil, nil),
		post("Bad", "next tuesday", nil),
		{Slug: "about", URL: "/about/", Title: "About", SourcePath: "R/About.md", Frontmatter: map[string]any{"date": "2026-12-01"}},
	}
	out, warnings := feedSite(t, &config.Feed{From: "Posts", Limit: 3}, pages...)

	var doc atomFeed
	if err := xml.Unmarshal([]byte(readOut(t, out, "feed.xml")), &doc); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range doc.Entries {
		got = append(got, e.Title+" "+e.Updated)
	}
	want := []string{"Newest 2026-11-10T15:04:00Z", "Tie-a 2026-06-01T00:00:00Z", "Tie-b 2026-06-01T00:00:00Z"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("entries (newest first, ties by URL, limit 3, no folder note or other folders):\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if doc.Title != "My Site" || doc.Author.Name != "My Site" || doc.ID != "https://example.org/" {
		t.Errorf("feed title/author/id = %q/%q/%q", doc.Title, doc.Author.Name, doc.ID)
	}
	if doc.Updated != "2026-11-10T15:04:00Z" {
		t.Errorf("feed updated = %q, want the newest entry's date, never the build time", doc.Updated)
	}
	if e := doc.Entries[0]; e.ID != "https://example.org/posts/newest/" || e.Link.Href != e.ID || e.Content != nil ||
		!strings.HasPrefix(e.Summary, "The body of Newest") {
		t.Errorf("entry = %+v", e)
	}
	if len(warnings) != 2 || !strings.Contains(strings.Join(warnings, "\n"), `R/Posts/Undated.md: left out of the feed: no usable "date" date`) ||
		!strings.Contains(strings.Join(warnings, "\n"), `R/Posts/Bad.md: left out`) {
		t.Errorf("warnings = %q", warnings)
	}
	if page := readOut(t, out, "posts/old/index.html"); !strings.Contains(page,
		`<link rel="alternate" type="application/atom+xml" title="My Site" href="/feed.xml">`) {
		t.Error("pages don't link the feed")
	}

	// A summary from frontmatter is escaped, not markup.
	out, _ = feedSite(t, &config.Feed{From: "Posts"}, pages[1])
	if raw := readOut(t, out, "feed.xml"); !strings.Contains(raw, "<summary>Old &amp; short &lt;summary&gt;.</summary>") {
		t.Errorf("summary not escaped:\n%s", raw)
	}
}

func TestFeedFullContent(t *testing.T) {
	out, _ := feedSite(t, &config.Feed{From: "Posts", Content: "full", Path: "blog/atom.xml", Title: "Posts feed"}, post("A", "2026-01-02", nil))
	var doc atomFeed
	if err := xml.Unmarshal([]byte(readOut(t, out, "blog/atom.xml")), &doc); err != nil {
		t.Fatal(err)
	}
	c := doc.Entries[0].Content
	if c == nil || c.Type != "html" {
		t.Fatalf("content = %+v", c)
	}
	for _, want := range []string{`href="https://example.org/posts/"`, `src="https://example.org/assets/x.png"`, `href="//cdn.example/y"`} {
		if !strings.Contains(c.Body, want) {
			t.Errorf("content lacks %s:\n%s", want, c.Body)
		}
	}
	if doc.Title != "Posts feed" || doc.Links[0].Href != "https://example.org/blog/atom.xml" {
		t.Errorf("title %q, self link %q", doc.Title, doc.Links[0].Href)
	}
	if !strings.Contains(readOut(t, out, "posts/a/index.html"), `title="Posts feed" href="/blog/atom.xml"`) {
		t.Error("page link doesn't use the feed's path and title")
	}
}

func TestFeedIsStableAndEmptyFeedIsValid(t *testing.T) {
	a, _ := feedSite(t, &config.Feed{From: "Posts"}, post("A", "2026-01-02", nil), post("B", "2026-01-03", nil))
	b, _ := feedSite(t, &config.Feed{From: "Posts"}, post("A", "2026-01-02", nil), post("B", "2026-01-03", nil))
	if readOut(t, a, "feed.xml") != readOut(t, b, "feed.xml") {
		t.Error("two builds of the same notes wrote different feeds")
	}
	out, _ := feedSite(t, &config.Feed{From: "Posts"})
	var doc atomFeed
	if err := xml.Unmarshal([]byte(readOut(t, out, "feed.xml")), &doc); err != nil || doc.Updated == "" || len(doc.Entries) != 0 {
		t.Errorf("empty feed: %v %+v", err, doc)
	}
}

func TestNoFeedUnlessConfigured(t *testing.T) {
	out, _ := redirectSite(t, &render.Page{Slug: "a", URL: "/a/", Title: "A", SourcePath: "R/a.md"})
	if _, err := os.Stat(filepath.Join(out, "feed.xml")); err == nil {
		t.Error("feed.xml written without site.feed")
	}
	if strings.Contains(readOut(t, out, "a/index.html"), "application/atom+xml") {
		t.Error("page links a feed that doesn't exist")
	}
}
