package render

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/deweysasser/obsidian-publish-website/internal/mdscan"
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

var h1Re = regexp.MustCompile(`^#\s+(.+?)(?:\s+#+)?\s*$`)

// Render renders one published note to target Markdown. Call Finish (after
// any transforms) to produce HTML.
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
		if title, rest, ok := liftH1(body, n.BodyLine); ok {
			page.Title, body = title, rest
		}
	}
	mdText, err := r.Preprocess(body, n.BodyLine, Context{Path: p, Stack: []string{p}})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	page.Markdown = mdText
	for _, src := range ip.Inlinks {
		if r.Pub.IsPublished(src) {
			page.Backlinks = append(page.Backlinks, r.Res.Note(src, "", ""))
		}
	}
	return page, nil
}

// Finish derives HTML and the TOC from the (possibly transformed) Markdown.
// Call it after transforms; it is a no-op outside ModeHTML.
func (r *Renderer) Finish(page *Page) error {
	if r.Mode != ModeHTML {
		return nil
	}
	var err error
	page.HTML, page.TOC, err = r.MarkdownToHTML(page.Markdown)
	if err != nil {
		return fmt.Errorf("%s: %w", page.SourcePath, err)
	}
	return nil
}

// liftH1 removes a leading "# Title" line (keeping line numbering intact).
// It looks only at comment-stripped lines, so a heading inside %% is never lifted.
func liftH1(body []byte, firstLine int) (string, []byte, bool) {
	for _, l := range mdscan.StripComments(mdscan.Lines(body, firstLine)) {
		if strings.TrimSpace(l.Text) == "" {
			continue
		}
		m := h1Re.FindStringSubmatch(l.Text)
		if m == nil || l.InFence || l.FenceOpen {
			return "", body, false
		}
		lines := strings.SplitAfter(string(body), "\n")
		i := l.Num - firstLine
		if i < 0 || i >= len(lines) || strings.TrimRight(lines[i], "\r\n") != l.Text {
			return "", body, false // the line was altered by comment stripping
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
