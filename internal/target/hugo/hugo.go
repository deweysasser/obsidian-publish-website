// Package hugo exports published pages as content for an existing Hugo site.
package hugo

import (
	"fmt"
	"path"
	"strings"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/deweysasser/obsidian-publish-website/internal/render"
	"github.com/deweysasser/obsidian-publish-website/internal/selector"
	"github.com/deweysasser/obsidian-publish-website/internal/target"
	"github.com/deweysasser/obsidian-publish-website/internal/urls"
	"gopkg.in/yaml.v3"
)

// Target writes content/<section>/ and static/<section>/ under the output dir.
type Target struct {
	cfg     *config.Config
	section string
	scheme  urls.Basic
	content string // output-relative
	static  string
	Warn    func(string)
	folders map[string]bool // slugs that are folders (branch bundles)
}

// New creates the hugo target.
func New(cfg *config.Config) *Target {
	base := urls.BasePathOf(cfg.Site.BaseURL)
	sec := cfg.Hugo.Section
	return &Target{
		cfg: cfg, section: sec,
		scheme:  urls.Basic{PagePrefix: base + sec + "/", AssetPrefix: base + sec + "/assets/", TagPrefix: base + "tags/"},
		content: path.Join(cfg.Output, "content", sec),
		static:  path.Join(cfg.Output, "static", sec),
		Warn:    func(string) {},
	}
}

func (t *Target) Scheme() urls.Scheme { return t.scheme }
func (t *Target) Mode() render.Mode   { return render.ModeMarkdown }

// Prepare empties only the directories this program owns.
func (t *Target) Prepare() error {
	if err := target.CleanDir(t.content); err != nil {
		return err
	}
	return target.CleanDir(t.static)
}

func (t *Target) pageFile(s *target.Site, slug string) string {
	if t.folders == nil {
		t.folders = map[string]bool{}
		s.Tree.Walk(func(n *target.FolderNode) { t.folders[n.Slug] = true })
	}
	if slug == "" {
		return "_index.md"
	}
	if t.folders[slug] {
		return slug + "/_index.md"
	}
	return slug + "/index.md"
}

// WritePage writes one page as a leaf or branch bundle.
func (t *Target) WritePage(s *target.Site, p *render.Page) error {
	fm := yaml.Node{Kind: yaml.MappingNode}
	add := func(k string, v any) {
		var n yaml.Node
		_ = n.Encode(v)
		fm.Content = append(fm.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, &n)
	}
	add("title", p.Title)
	if !p.Mtime.IsZero() {
		add("date", p.Mtime.Format("2006-01-02T15:04:05Z07:00"))
		add("lastmod", p.Mtime.Format("2006-01-02T15:04:05Z07:00"))
	}
	add("url", "/"+t.section+"/"+strings.TrimPrefix(p.Slug+"/", "/"))
	if len(p.Tags) > 0 {
		var tags []string
		for _, tg := range p.Tags {
			tags = append(tags, selector.Slugify(strings.TrimPrefix(tg, "#")))
		}
		add("tags", tags)
	}
	if len(p.Frontmatter) > 0 {
		add("obsidian", target.ExportFrontmatter(p.Frontmatter))
	}
	return t.write(t.pageFile(s, p.Slug), &fm, p.Markdown)
}

func (t *Target) write(rel string, fm *yaml.Node, body string) error {
	head, err := yaml.Marshal(fm)
	if err != nil {
		return err
	}
	doc := "---\n" + string(head) + "---\n\n" + escapeShortcodes(body)
	return target.WriteFile(t.content, rel, []byte(doc))
}

// escapeShortcodes neutralizes Hugo shortcode syntax in note text.
func escapeShortcodes(s string) string {
	return strings.NewReplacer("{{<", "{{</*", ">}}", "*/>}}", "{{%", "{{%/*", "%}}", "*/%}}").Replace(s)
}

// CopyAsset copies an attachment into static/<section>/assets/.
func (t *Target) CopyAsset(a target.Asset) error {
	return target.CopyFile(a.Src, t.static, "assets/"+a.Slug)
}

// Finalize writes _index.md for folders that have no folder note.
func (t *Target) Finalize(s *target.Site) error {
	var err error
	s.Tree.Walk(func(n *target.FolderNode) {
		if err != nil || n.Page != nil {
			return
		}
		fm := yaml.Node{Kind: yaml.MappingNode}
		title := n.Name
		if n.Slug == "" {
			title = s.Config.Site.Title
		}
		for _, kv := range [][2]string{{"title", title}, {"url", "/" + t.section + "/" + strings.TrimPrefix(n.Slug+"/", "/")}} {
			fm.Content = append(fm.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: kv[0]}, &yaml.Node{Kind: yaml.ScalarNode, Value: kv[1]})
		}
		err = t.write(t.pageFile(s, n.Slug), &fm, "")
	})
	if err != nil {
		return err
	}
	t.Warn(fmt.Sprintf("hugo: exported %d pages to %s; set markup.goldmark.renderer.unsafe = true in the Hugo config so callouts and dataview tables render", len(s.Pages), t.content))
	return nil
}

var _ target.Target = (*Target)(nil)
