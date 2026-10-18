package render

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/deweysasser/obsidian-publish-website/internal/resolve"
)

// Page is a rendered, target-ready page.
type Page struct {
	SourcePath  string
	Slug        string
	URL         string
	Title       string
	Frontmatter map[string]any
	Tags        []string
	Markdown    string // target-flavored markdown
	HTML        string // only in ModeHTML
	TOC         []TOCEntry
	Backlinks   []resolve.Result
	Mtime       time.Time
}

var h1Re = regexp.MustCompile(`^#\s+(.+?)\s*#*\s*$`)

// Render renders one published note.
func (r *Renderer) Render(p string) (*Page, error) {
	ip := r.Ix.Pages[p]
	slug, ok := r.Pub.Pages[p]
	if ip == nil || !ok {
		return nil, fmt.Errorf("%s: not a published note", p)
	}
	n := ip.Note
	page := &Page{SourcePath: p, Slug: slug, URL: r.URLs.PageURL(slug), Frontmatter: n.Frontmatter,
		Tags: ip.ETags, Mtime: n.Mtime}
	body := n.Body
	if t, ok := n.Frontmatter["title"].(string); ok && strings.TrimSpace(t) != "" {
		page.Title = strings.TrimSpace(t)
	} else {
		page.Title = strings.TrimSuffix(path.Base(p), path.Ext(p))
		if title, rest, ok := liftH1(body); ok {
			page.Title, body = title, rest
		}
	}
	mdText, err := r.Preprocess(body, n.BodyLine, Context{Path: p})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	page.Markdown = mdText
	if r.Mode == ModeHTML {
		page.HTML, page.TOC, err = r.MarkdownToHTML(mdText)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}
	for _, src := range ip.Inlinks {
		if r.Pub.IsPublished(src) {
			page.Backlinks = append(page.Backlinks, r.Res.Note(src, "", ""))
		}
	}
	return page, nil
}

// liftH1 removes a leading "# Title" line (keeping line numbering intact).
func liftH1(body []byte) (string, []byte, bool) {
	lines := strings.SplitAfter(string(body), "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		m := h1Re.FindStringSubmatch(strings.TrimRight(l, "\r\n"))
		if m == nil {
			return "", body, false
		}
		lines[i] = "\n"
		return plainTitle(m[1]), []byte(strings.Join(lines, "")), true
	}
	return "", body, false
}

var titleLinkRe = regexp.MustCompile(`\[\[(?:[^\]|]*\|)?([^\]]*)\]\]`)

func plainTitle(s string) string {
	s = titleLinkRe.ReplaceAllString(s, "$1")
	return strings.NewReplacer("**", "", "__", "", "==", "", "`", "").Replace(s)
}
