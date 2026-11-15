package html

import (
	"encoding/xml"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/deweysasser/obsidian-publish-website/internal/render"
	"github.com/deweysasser/obsidian-publish-website/internal/target"
)

// Atom (RFC 4287) documents, as encoding/xml writes them.
type atomFeed struct {
	XMLName xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Title   string      `xml:"title"`
	ID      string      `xml:"id"`
	Updated string      `xml:"updated"`
	Links   []atomLink  `xml:"link"`
	Author  atomAuthor  `xml:"author"`
	Entries []atomEntry `xml:"entry"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr,omitempty"`
	Type string `xml:"type,attr,omitempty"`
	Href string `xml:"href,attr"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

type atomEntry struct {
	Title     string    `xml:"title"`
	ID        string    `xml:"id"`
	Link      atomLink  `xml:"link"`
	Published string    `xml:"published"`
	Updated   string    `xml:"updated"`
	Summary   string    `xml:"summary,omitempty"`
	Content   *atomText `xml:"content"`
}

type atomText struct {
	Type string `xml:"type,attr"`
	Body string `xml:",chardata"`
}

// feedEntry is a note chosen for the feed, with its date.
type feedEntry struct {
	page *render.Page
	date time.Time
}

// feedURL is the feed's path on the site ("" without site.feed).
func (t *Target) feedURL(s *target.Site) string {
	if f := s.Config.Site.Feed; f != nil {
		return t.base + f.Path
	}
	return ""
}

// writeFeed publishes site.feed. Its dates all come from the notes, never
// the build time, so a rebuild of an unchanged vault writes the same file.
func (t *Target) writeFeed(s *target.Site) error {
	f := s.Config.Site.Feed
	if f == nil {
		return nil
	}
	if _, err := os.Stat(filepath.Join(t.out, filepath.FromSlash(f.Path))); err == nil {
		return fmt.Errorf("site.feed.path %q: a published page or file is already there", f.Path)
	}
	entries := t.feedEntries(s, f)
	title := f.Title
	if title == "" {
		title = s.Config.Site.Title
	}
	author := f.Author
	if author == "" {
		author = s.Config.Site.Title
	}
	home := t.origin + t.base
	doc := atomFeed{
		Title: title, ID: home,
		Links: []atomLink{
			{Rel: "self", Type: "application/atom+xml", Href: t.origin + t.feedURL(s)},
			{Rel: "alternate", Type: "text/html", Href: home},
		},
		Author: atomAuthor{author},
	}
	var newest time.Time
	for _, e := range entries {
		when := e.date.Format(time.RFC3339)
		if e.date.After(newest) {
			newest, doc.Updated = e.date, when
		}
		u := t.origin + e.page.URL
		ae := atomEntry{Title: e.page.Title, ID: u, Link: atomLink{Rel: "alternate", Type: "text/html", Href: u},
			Published: when, Updated: when, Summary: describe(e.page)}
		if f.Content == "full" {
			ae.Content = &atomText{Type: "html", Body: absoluteURLs(e.page.HTML, t.origin)}
		}
		doc.Entries = append(doc.Entries, ae)
	}
	if doc.Updated == "" {
		// An empty feed still needs <updated>; a fixed date keeps it stable.
		doc.Updated = time.Unix(0, 0).UTC().Format(time.RFC3339)
	}
	b, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return target.WriteFile(t.out, f.Path, append([]byte(xml.Header), append(b, '\n')...))
}

// feedEntries returns the newest notes under site.feed.from, newest first
// (ties by URL, so the order never depends on the build).
func (t *Target) feedEntries(s *target.Site, f *config.Feed) []feedEntry {
	folder := path.Join(s.Config.Root, f.From)
	var folderNote *render.Page
	s.Tree.Walk(func(n *target.FolderNode) {
		if n.Folder == folder {
			folderNote = n.Page
		}
	})
	var entries []feedEntry
	for _, p := range s.Pages {
		if !strings.HasPrefix(p.SourcePath, folder+"/") || p == folderNote {
			continue
		}
		d, ok := feedDate(p.Frontmatter[f.Date])
		if !ok {
			t.Warn(fmt.Sprintf("%s: left out of the feed: no usable %q date", p.SourcePath, f.Date))
			continue
		}
		entries = append(entries, feedEntry{p, d})
	}
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].date.Equal(entries[j].date) {
			return entries[i].date.After(entries[j].date)
		}
		return entries[i].page.URL < entries[j].page.URL
	})
	if len(entries) > f.Limit {
		entries = entries[:f.Limit]
	}
	return entries
}

// feedDateLayouts are the string dates a note may give, after YAML's own
// timestamps. A date without a zone is taken as UTC.
var feedDateLayouts = []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"}

func feedDate(v any) (time.Time, bool) {
	switch d := v.(type) {
	case time.Time:
		return d, !d.IsZero()
	case string:
		s := strings.TrimSpace(d)
		for _, layout := range feedDateLayouts {
			if t, err := time.Parse(layout, s); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

// rootRelativeRe finds href and src attributes holding a root-relative URL.
var rootRelativeRe = regexp.MustCompile(`\b(href|src)="/([^/"][^"]*)?"`)

// absoluteURLs makes a page's root-relative links absolute, since a feed
// reader resolves them against the feed, not the page.
func absoluteURLs(pageHTML, origin string) string {
	return rootRelativeRe.ReplaceAllString(pageHTML, `$1="`+origin+`/$2"`)
}
