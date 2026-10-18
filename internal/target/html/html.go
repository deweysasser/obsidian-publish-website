// Package html writes a complete, self-contained static site.
package html

import (
	"bytes"
	"embed"
	"encoding/xml"
	"fmt"
	"html/template"
	"io/fs"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/deweysasser/obsidian-publish-website/internal/render"
	"github.com/deweysasser/obsidian-publish-website/internal/resolve"
	"github.com/deweysasser/obsidian-publish-website/internal/selector"
	"github.com/deweysasser/obsidian-publish-website/internal/target"
	"github.com/deweysasser/obsidian-publish-website/internal/urls"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed all:assets
var assetFS embed.FS

// Target is the html output target.
type Target struct {
	cfg    *config.Config
	base   string
	scheme urls.Basic
	tmpl   *template.Template
	out    string
	// Warn receives non-fatal problems.
	Warn func(string)
}

// New creates the html target.
func New(cfg *config.Config) (*Target, error) {
	base := urls.BasePathOf(cfg.Site.BaseURL)
	t := &Target{cfg: cfg, base: base, scheme: urls.HTML(base), out: cfg.Output, Warn: func(string) {}}
	tmpl, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	if dir := cfg.HTML.TemplateDir; dir != "" {
		if tmpl, err = tmpl.ParseGlob(filepath.Join(dir, "*.html")); err != nil {
			return nil, fmt.Errorf("html.template_dir: %w", err)
		}
	}
	t.tmpl = tmpl
	return t, nil
}

func (t *Target) Scheme() urls.Scheme { return t.scheme }
func (t *Target) Mode() render.Mode   { return render.ModeHTML }
func (t *Target) Prepare() error      { return target.CleanDir(t.out) }

type crumb struct{ Name, URL string }
type tagLink struct{ Name, URL string }

type view struct {
	SiteTitle, Base, Title string
	Content                template.HTML
	Nav                    template.HTML
	TOC                    []render.TOCEntry
	Backlinks              []resolve.Result
	Crumbs                 []crumb
	Tags                   []tagLink
	Modified               string
	Search, Math, Mermaid  bool
	Indexed                bool
}

func (t *Target) write(s *target.Site, slug string, v view, current string) error {
	v.SiteTitle, v.Base, v.Search = s.Config.Site.Title, t.base, s.Config.Search == "pagefind"
	v.Nav = navHTML(s.Tree, current)
	v.Math = strings.Contains(string(v.Content), `class="math`)
	v.Mermaid = strings.Contains(string(v.Content), `class="mermaid"`)
	var buf bytes.Buffer
	if err := t.tmpl.ExecuteTemplate(&buf, "layout.html", v); err != nil {
		return err
	}
	rel := "index.html"
	if slug == "404" {
		rel = "404.html"
	} else if slug != "" {
		rel = slug + "/index.html"
	}
	return target.WriteFile(t.out, rel, buf.Bytes())
}

// WritePage writes one note page.
func (t *Target) WritePage(s *target.Site, p *render.Page) error {
	v := view{Title: p.Title, Content: template.HTML(p.HTML), TOC: tocWithout(p.TOC), Backlinks: p.Backlinks,
		Crumbs: crumbs(s, p.SourcePath, p.Slug), Indexed: true}
	if !p.Mtime.IsZero() {
		v.Modified = p.Mtime.Format("2006-01-02")
	}
	for _, tg := range p.Tags {
		v.Tags = append(v.Tags, tagLink{tg, t.scheme.TagURL(tg)})
	}
	// a folder note without its own content still gets a listing
	if node := findNode(s.Tree, p.Slug); node != nil && node.Page == p && strings.TrimSpace(p.Markdown) == "" {
		v.Content = listingHTML(node)
	}
	return t.write(s, p.Slug, v, p.URL)
}

func tocWithout(toc []render.TOCEntry) []render.TOCEntry {
	if len(toc) < 2 {
		return nil
	}
	return toc
}

// CopyAsset copies one attachment.
func (t *Target) CopyAsset(a target.Asset) error {
	return target.CopyFile(a.Src, t.out, "assets/"+a.Slug)
}

// Finalize writes generated pages, static assets and the sitemap.
func (t *Target) Finalize(s *target.Site) error {
	var err error
	s.Tree.Walk(func(n *target.FolderNode) {
		if err == nil && n.Page == nil {
			title := n.Name
			if n.Slug == "" {
				title = s.Config.Site.Title
			}
			err = t.write(s, n.Slug, view{Title: title, Content: listingHTML(n), Crumbs: crumbs(s, n.Folder+"/x", n.Slug), Indexed: false}, n.URL)
		}
	})
	if err != nil {
		return err
	}
	if err := t.write(s, "all", view{Title: "All pages", Content: allPagesHTML(s.Tree)}, t.scheme.PageURL("all")); err != nil {
		return err
	}
	if err := t.writeTags(s); err != nil {
		return err
	}
	if err := t.write(s, "404", view{Title: "Not found", Content: template.HTML(`<p>That page does not exist. Try the <a href="` + t.base + `all/">list of all pages</a>.</p>`)}, ""); err != nil {
		return err
	}
	if err := target.WriteFile(t.out, ".nojekyll", nil); err != nil {
		return err
	}
	if err := fs.WalkDir(assetFS, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := assetFS.ReadFile(p)
		if err != nil {
			return err
		}
		return target.WriteFile(t.out, p, b)
	}); err != nil {
		return err
	}
	return t.writeSitemap(s)
}

func (t *Target) writeTags(s *target.Site) error {
	var names []string
	for tg := range s.Tags {
		names = append(names, tg)
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	var b strings.Builder
	b.WriteString(`<ul class="listing tags">`)
	for _, tg := range names {
		fmt.Fprintf(&b, `<li><a class="tag" href="%s">%s</a> <span class="count">%d</span></li>`,
			template.HTMLEscapeString(t.scheme.TagURL(tg)), template.HTMLEscapeString(tg), len(s.Tags[tg]))
	}
	b.WriteString("</ul>")
	if err := t.write(s, "tags", view{Title: "Tags", Content: template.HTML(b.String())}, t.scheme.PageURL("tags")); err != nil {
		return err
	}
	for _, tg := range names {
		var pb strings.Builder
		pb.WriteString(`<ul class="listing">`)
		for _, p := range s.Tags[tg] {
			fmt.Fprintf(&pb, `<li><a href="%s">%s</a></li>`, template.HTMLEscapeString(p.URL), template.HTMLEscapeString(p.Title))
		}
		pb.WriteString("</ul>")
		slug := "tags/" + selector.Slugify(strings.TrimPrefix(tg, "#"))
		if err := t.write(s, slug, view{Title: tg, Content: template.HTML(pb.String())}, ""); err != nil {
			return err
		}
	}
	return nil
}

type urlEntry struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

func (t *Target) writeSitemap(s *target.Site) error {
	u, err := url.Parse(s.Config.Site.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		t.Warn("site.base_url is not an absolute URL; sitemap.xml not written")
		return nil
	}
	origin := u.Scheme + "://" + u.Host
	var entries []urlEntry
	seen := map[string]bool{}
	for _, p := range s.Pages {
		entries = append(entries, urlEntry{Loc: origin + p.URL, LastMod: p.Mtime.Format("2006-01-02")})
		seen[p.URL] = true
	}
	s.Tree.Walk(func(n *target.FolderNode) {
		if !seen[n.URL] {
			entries = append(entries, urlEntry{Loc: origin + n.URL})
		}
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].Loc < entries[j].Loc })
	out, err := xml.MarshalIndent(struct {
		XMLName xml.Name   `xml:"urlset"`
		NS      string     `xml:"xmlns,attr"`
		URLs    []urlEntry `xml:"url"`
	}{NS: "http://www.sitemaps.org/schemas/sitemap/0.9", URLs: entries}, "", "  ")
	if err != nil {
		return err
	}
	return target.WriteFile(t.out, "sitemap.xml", append([]byte(xml.Header), out...))
}

func findNode(root *target.FolderNode, slug string) *target.FolderNode {
	var found *target.FolderNode
	root.Walk(func(n *target.FolderNode) {
		if n.Slug == slug {
			found = n
		}
	})
	return found
}

// crumbs returns links to the ancestor folders of a page.
func crumbs(s *target.Site, sourcePath, slug string) []crumb {
	if slug == "" {
		return nil
	}
	byFolder := map[string]*target.FolderNode{}
	s.Tree.Walk(func(n *target.FolderNode) { byFolder[n.Folder] = n })
	var out []crumb
	for f := path.Dir(sourcePath); ; f = path.Dir(f) {
		if n := byFolder[f]; n != nil && n.Slug != slug {
			name := n.Name
			if n.Slug == "" {
				name = "Home"
			}
			out = append([]crumb{{name, n.URL}}, out...)
		}
		if f == s.Tree.Folder || f == "." || f == "/" {
			break
		}
	}
	return out
}

func navHTML(root *target.FolderNode, current string) template.HTML {
	var b strings.Builder
	var walk func(n *target.FolderNode)
	link := func(url, title string) {
		cur := ""
		if url == current {
			cur = ` aria-current="page"`
		}
		fmt.Fprintf(&b, `<a href="%s"%s>%s</a>`, template.HTMLEscapeString(url), cur, template.HTMLEscapeString(title))
	}
	contains := func(n *target.FolderNode) bool {
		hit := false
		n.Walk(func(m *target.FolderNode) {
			if m.URL == current {
				hit = true
			}
			for _, p := range m.Pages {
				if p.URL == current {
					hit = true
				}
			}
		})
		return hit
	}
	walk = func(n *target.FolderNode) {
		b.WriteString("<ul>")
		for _, f := range n.Folders {
			open := ""
			if contains(f) {
				open = " open"
			}
			fmt.Fprintf(&b, `<li class="folder"><details%s><summary>`, open)
			link(f.URL, f.Name)
			b.WriteString("</summary>")
			walk(f)
			b.WriteString("</details></li>")
		}
		for _, p := range n.Pages {
			b.WriteString("<li>")
			link(p.URL, p.Title)
			b.WriteString("</li>")
		}
		b.WriteString("</ul>")
	}
	walk(root)
	return template.HTML(b.String())
}

func listingHTML(n *target.FolderNode) template.HTML {
	var b strings.Builder
	b.WriteString(`<ul class="listing">`)
	for _, f := range n.Folders {
		fmt.Fprintf(&b, `<li class="folder"><a href="%s">%s</a></li>`, template.HTMLEscapeString(f.URL), template.HTMLEscapeString(f.Name))
	}
	for _, p := range n.Pages {
		fmt.Fprintf(&b, `<li><a href="%s">%s</a></li>`, template.HTMLEscapeString(p.URL), template.HTMLEscapeString(p.Title))
	}
	b.WriteString("</ul>")
	return template.HTML(b.String())
}

func allPagesHTML(root *target.FolderNode) template.HTML {
	var b strings.Builder
	root.Walk(func(n *target.FolderNode) {
		var pages []*render.Page
		if n.Page != nil {
			pages = append(pages, n.Page)
		}
		pages = append(pages, n.Pages...)
		if len(pages) == 0 {
			return
		}
		name := n.Name
		if n.Slug == "" {
			name = "Home"
		}
		fmt.Fprintf(&b, `<h2><a href="%s">%s</a></h2><ul class="listing">`, template.HTMLEscapeString(n.URL), template.HTMLEscapeString(name))
		for _, p := range pages {
			fmt.Fprintf(&b, `<li><a href="%s">%s</a></li>`, template.HTMLEscapeString(p.URL), template.HTMLEscapeString(p.Title))
		}
		b.WriteString("</ul>")
	})
	return template.HTML(b.String())
}

var _ target.Target = (*Target)(nil)
