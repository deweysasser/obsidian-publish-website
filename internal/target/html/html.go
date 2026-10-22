// Package html writes a complete, self-contained static site.
package html

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
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
	origin string // scheme://host of base_url, "" when it is not absolute
	nav    *nav
	// versioned maps a static asset to its content-addressed copy, which
	// browsers may cache forever.
	versioned map[string]string
	// Warn receives non-fatal problems.
	Warn func(string)
}

// New creates the html target.
func New(cfg *config.Config) (*Target, error) {
	base := urls.BasePathOf(cfg.Site.BaseURL)
	t := &Target{cfg: cfg, base: base, scheme: urls.HTML(base), out: cfg.Output, Warn: func(string) {}}
	if u, err := url.Parse(cfg.Site.BaseURL); err == nil && u.Scheme != "" && u.Host != "" {
		t.origin = u.Scheme + "://" + u.Host
	}
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
	t.versioned = map[string]string{}
	for _, name := range []string{"style.css", "app.js"} {
		b, err := assetFS.ReadFile("assets/" + name)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		ext := path.Ext(name)
		t.versioned[name] = "assets/_v/" + strings.TrimSuffix(name, ext) + "." + hex.EncodeToString(sum[:5]) + ext
	}
	return t, nil
}

func (t *Target) Scheme() urls.Scheme { return t.scheme }
func (t *Target) Mode() render.Mode   { return render.ModeHTML }
func (t *Target) Prepare() error      { return target.CleanDir(t.out) }

type crumb struct{ Name, URL string }
type tagLink struct{ Name, URL string }

type view struct {
	SiteTitle, Base, Title string
	// page metadata for search engines and link previews
	Description, Canonical, Image, Favicon string
	Style, Script                          string
	HasTags                                bool
	Content                                template.HTML
	Nav                                    template.HTML
	TOC                                    []render.TOCEntry
	Backlinks                              []resolve.Result
	Crumbs                                 []crumb
	Tags                                   []tagLink
	Modified                               string
	Search, Math, Mermaid                  bool
	Indexed                                bool
}

func (t *Target) write(s *target.Site, slug string, v view, current string) error {
	v.SiteTitle, v.Base, v.Search = s.Config.Site.Title, t.base, s.Config.Search == "pagefind"
	if t.origin != "" && current != "" {
		v.Canonical = t.origin + current
	}
	v.HasTags = len(s.Tags) > 0
	v.Style, v.Script = t.base+t.versioned["style.css"], t.base+t.versioned["app.js"]
	v.Favicon = t.base + "assets/favicon.svg"
	if f := s.Config.Site.Favicon; f != "" {
		v.Favicon = t.base + faviconName(f)
	}
	if t.nav == nil || t.nav.root != s.Tree {
		t.nav = newNav(s.Tree)
	}
	v.Nav = t.nav.html(current)
	v.Math = strings.Contains(string(v.Content), `class="math`)
	v.Mermaid = strings.Contains(string(v.Content), `class="mermaid"`)
	var buf bytes.Buffer
	if err := t.tmpl.ExecuteTemplate(&buf, "layout.html", v); err != nil {
		return err
	}
	rel := "index.html"
	if slug == notFoundSlug {
		rel = "404.html"
	} else if slug != "" {
		rel = slug + "/index.html"
	}
	return target.WriteFile(t.out, rel, buf.Bytes())
}

// notFoundSlug marks the generated 404 page; it cannot collide with a note slug.
const notFoundSlug = "\x00404"

// reserved top-level slugs used by generated pages and static files.
var reserved = map[string]bool{"tags": true, "assets": true, "pagefind": true}

// WritePage writes one note page.
func (t *Target) WritePage(s *target.Site, p *render.Page) error {
	if top, _, _ := strings.Cut(p.Slug, "/"); reserved[top] {
		return fmt.Errorf("%s: its URL /%s/ is reserved for generated pages in the html target; rename the note or folder", p.SourcePath, top)
	}
	v := view{Title: p.Title, Content: template.HTML(p.HTML), TOC: tocWithout(p.TOC), Backlinks: p.Backlinks,
		Crumbs: crumbs(s, p.SourcePath, p.Slug), Indexed: true, Description: describe(p), Image: shareImage(p.HTML)}
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
	if err := t.writeTags(s); err != nil {
		return err
	}
	if err := t.write(s, notFoundSlug, view{Title: "Not found", Content: template.HTML(`<p>That page does not exist. Try the <a href="` + t.base + `">home page</a> or search.</p>`)}, ""); err != nil {
		return err
	}
	if err := target.WriteFile(t.out, ".nojekyll", nil); err != nil {
		return err
	}
	if f := s.Config.Site.Favicon; f != "" {
		if err := target.CopyFile(filepath.Join(s.Config.Vault, f), t.out, faviconName(f)); err != nil {
			return fmt.Errorf("site.favicon: %w", err)
		}
	}
	if err := fs.WalkDir(assetFS, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := assetFS.ReadFile(p)
		if err != nil {
			return err
		}
		if v := t.versioned[strings.TrimPrefix(p, "assets/")]; v != "" {
			if err := target.WriteFile(t.out, v, b); err != nil {
				return err
			}
		}
		return target.WriteFile(t.out, p, b) // unversioned too, for custom templates
	}); err != nil {
		return err
	}
	if err := target.WriteFile(t.out, "_headers", []byte(cacheHeaders)); err != nil {
		return err
	}
	return t.writeSitemap(s)
}

// cacheHeaders tells Cloudflare Pages (and Netlify) which files never change
// under their name: our content-addressed assets and Pagefind's hashed index.
const cacheHeaders = `/assets/_v/*
  Cache-Control: public, max-age=31536000, immutable
/pagefind/fragment/*
  Cache-Control: public, max-age=31536000, immutable
/pagefind/index/*
  Cache-Control: public, max-age=31536000, immutable
/assets/vendor/*
  Cache-Control: public, max-age=86400
`

// faviconName is where a configured favicon is published.
func faviconName(src string) string { return "favicon" + strings.ToLower(path.Ext(src)) }

// writeRobots points crawlers at the sitemap (which needs an absolute base_url).
func (t *Target) writeRobots(origin string) error {
	return target.WriteFile(t.out, "robots.txt", []byte("User-agent: *\nAllow: /\nSitemap: "+origin+t.base+"sitemap.xml\n"))
}

func (t *Target) writeTags(s *target.Site) error {
	var names []string
	for tg := range s.Tags {
		names = append(names, tg)
	}
	if len(names) == 0 {
		return nil
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
		return nil // warned at build start
	}
	origin := u.Scheme + "://" + u.Host
	if err := t.writeRobots(origin); err != nil {
		return err
	}
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

// nav renders the site navigation for one page: every top-level entry, plus
// the contents of each folder on the path to the current page. Listing the
// whole tree on every page makes the site quadratic in size.
type nav struct {
	root   *target.FolderNode
	parent map[*target.FolderNode]*target.FolderNode
	owner  map[string]*target.FolderNode // page or folder URL → folder to expand
}

func newNav(root *target.FolderNode) *nav {
	n := &nav{root: root, parent: map[*target.FolderNode]*target.FolderNode{}, owner: map[string]*target.FolderNode{}}
	root.Walk(func(f *target.FolderNode) {
		n.owner[f.URL] = f
		for _, c := range f.Folders {
			n.parent[c] = f
		}
		for _, p := range f.Pages {
			n.owner[p.URL] = f
		}
	})
	return n
}

func (n *nav) html(current string) template.HTML {
	open := map[*target.FolderNode]bool{n.root: true}
	for f := n.owner[current]; f != nil; f = n.parent[f] {
		open[f] = true
	}
	var b strings.Builder
	link := func(url, title string) {
		cur := ""
		if url == current {
			cur = ` aria-current="page"`
		}
		fmt.Fprintf(&b, `<a href="%s"%s>%s</a>`, template.HTMLEscapeString(url), cur, template.HTMLEscapeString(title))
	}
	var walk func(f *target.FolderNode)
	walk = func(f *target.FolderNode) {
		b.WriteString("<ul>")
		for _, c := range f.Folders {
			if open[c] {
				b.WriteString(`<li class="folder open">`)
				link(c.URL, c.Name)
				walk(c)
			} else {
				b.WriteString(`<li class="folder">`)
				link(c.URL, c.Name)
			}
			b.WriteString("</li>")
		}
		for _, p := range f.Pages {
			b.WriteString("<li>")
			link(p.URL, p.Title)
			b.WriteString("</li>")
		}
		b.WriteString("</ul>")
	}
	walk(n.root)
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

var _ target.Target = (*Target)(nil)
