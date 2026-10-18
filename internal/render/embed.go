package render

import (
	"fmt"
	"html"
	"path"
	"strings"

	"github.com/deweysasser/obsidian-publish-website/internal/index"
	"github.com/deweysasser/obsidian-publish-website/internal/mdscan"
	"github.com/deweysasser/obsidian-publish-website/internal/resolve"
)

var (
	imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".svg": true, ".avif": true, ".bmp": true}
	audioExts = map[string]bool{".mp3": true, ".wav": true, ".ogg": true, ".m4a": true, ".flac": true, ".webm_audio": true}
	videoExts = map[string]bool{".mp4": true, ".webm": true, ".mov": true, ".ogv": true}
)

// DefaultMaxEmbedDepth matches Dataview/Obsidian's maxRecursiveRenderDepth default.
const DefaultMaxEmbedDepth = 4

func (r *Renderer) embed(l index.Link, line int, ctx Context) string {
	if l.Path == "" {
		r.Warn(ctx.Path, line, fmt.Sprintf("unresolved embed %q", l.Raw))
		return `<span class="embed embed-missing">Missing embed</span>`
	}
	if r.Ix.Pages[l.Path] == nil {
		return r.embedAsset(l, line, ctx)
	}
	return r.embedNote(l, line, ctx)
}

func (r *Renderer) embedAsset(l index.Link, line int, ctx Context) string {
	res := r.Res.Link(l, ctx.Path)
	if !res.Published {
		r.Warn(ctx.Path, line, fmt.Sprintf("embedded file %q is not published", l.Raw))
		return ""
	}
	src := html.EscapeString(res.URL)
	ext := strings.ToLower(path.Ext(l.Path))
	switch {
	case imageExts[ext]:
		alt := path.Base(l.Path)
		attrs := ""
		if l.Display != "" {
			if w, h, ok := strings.Cut(l.Display, "x"); isSizeSpec(l.Display) {
				attrs = fmt.Sprintf(` width="%s"`, w)
				if ok {
					attrs += fmt.Sprintf(` height="%s"`, h)
				}
			} else {
				alt = l.Display
			}
		}
		return fmt.Sprintf(`<img src="%s" alt="%s"%s>`, src, html.EscapeString(alt), attrs)
	case ext == ".pdf":
		return fmt.Sprintf(`<iframe class="embed-pdf" src="%s"></iframe>`, src)
	case audioExts[ext]:
		return fmt.Sprintf(`<audio controls src="%s"></audio>`, src)
	case videoExts[ext]:
		return fmt.Sprintf(`<video controls src="%s"></video>`, src)
	}
	return fmt.Sprintf(`<a class="embed-file" href="%s">%s</a>`, src, html.EscapeString(resolve.DisplayText(l)))
}

func isSizeSpec(s string) bool {
	w, h, ok := strings.Cut(s, "x")
	return isDigits(w) && (!ok || isDigits(h))
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (r *Renderer) embedNote(l index.Link, line int, ctx Context) string {
	slug, ok := r.Pub.Pages[l.Path]
	if !ok {
		return `<div class="embed embed-missing">Not published</div>`
	}
	max := r.MaxEmbedDepth
	if max <= 0 {
		max = DefaultMaxEmbedDepth
	}
	for _, s := range ctx.Stack {
		if s == l.Path {
			return `<div class="embed embed-missing">Embed depth exceeded</div>`
		}
	}
	if ctx.Depth >= max {
		return `<div class="embed embed-missing">Embed depth exceeded</div>`
	}
	body, first, ok := r.section(l.Path, l.Subpath)
	if !ok {
		r.Warn(ctx.Path, line, fmt.Sprintf("embed %q: section not found", l.Raw))
		return `<div class="embed embed-missing">Missing embed</div>`
	}
	inner := Context{Path: l.Path, Depth: ctx.Depth + 1, Stack: append(append([]string{}, ctx.Stack...), ctx.Path)}
	md, err := r.Preprocess([]byte(body), first, inner)
	if err != nil {
		r.Warn(ctx.Path, line, err.Error())
		return ""
	}
	return fmt.Sprintf("\n\n<div class=\"embed\" data-src=\"%s\">\n\n%s\n\n</div>\n\n",
		html.EscapeString(r.URLs.PageURL(slug)), strings.TrimRight(md, "\n"))
}

// section returns the embedded text of a note (whole body, a heading section
// or a block) and its first file line number.
func (r *Renderer) section(p, subpath string) (string, int, bool) {
	page := r.Ix.Pages[p]
	n := page.Note
	if subpath == "" {
		return string(n.Body), n.BodyLine, true
	}
	lines := strings.Split(string(n.Raw), "\n")
	// Slice the comment-stripped note so a section can never start inside %%.
	clean := mdscan.StripComments(mdscan.Lines(n.Raw, 1))
	get := func(from, to int) (string, int, bool) { // 1-based inclusive file lines
		var out []string
		for _, l := range clean {
			if l.Num >= from && l.Num <= to {
				out = append(out, l.Text)
			}
		}
		return strings.Join(out, "\n"), from, true
	}
	if strings.HasPrefix(subpath, "^") {
		at, ok := page.Blocks[subpath[1:]]
		if !ok {
			return "", 0, false
		}
		for i, it := range page.Lists {
			if it.Line == at {
				return get(at, listEnd(page, i))
			}
		}
		from, to := at, at
		for from > n.BodyLine && strings.TrimSpace(lines[from-2]) != "" && !strings.HasPrefix(lines[from-2], "#") {
			from--
		}
		for to < len(lines) && strings.TrimSpace(lines[to]) != "" {
			to++
		}
		return get(from, to)
	}
	if i := strings.LastIndex(subpath, "#"); i >= 0 {
		subpath = subpath[i+1:]
	}
	want := resolve.HeadingID(subpath)
	for i, h := range page.Headings {
		if resolve.HeadingID(h.Text) != want {
			continue
		}
		end := len(lines)
		for _, h2 := range page.Headings[i+1:] {
			if h2.Level <= h.Level {
				end = h2.Line - 1
				break
			}
		}
		return get(h.Line, end)
	}
	return "", 0, false
}

func listEnd(page *index.Page, i int) int {
	it := page.Lists[i]
	end := it.Line + it.LineCount - 1
	for _, c := range it.Children {
		end = max(end, listEnd(page, c))
	}
	return end
}
