package html

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/deweysasser/obsidian-publish-website/internal/render"
	"github.com/deweysasser/obsidian-publish-website/internal/target"
)

func TestVendorScriptsOnlyWhenNeeded(t *testing.T) {
	out := t.TempDir()
	cfg := &config.Config{Output: out, Search: "none", Site: config.Site{Title: "S"}}
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	site := &target.Site{Config: cfg, Tree: &target.FolderNode{}}
	pages := []*render.Page{
		{Slug: "m", URL: "/m/", Title: "M", HTML: `<span class="math">\(x\)</span><pre class="mermaid">a</pre>`},
		{Slug: "p", URL: "/p/", Title: "P", HTML: `<p>plain</p>`},
	}
	for _, p := range pages {
		if err := tg.WritePage(site, p); err != nil {
			t.Fatal(err)
		}
	}
	m, _ := os.ReadFile(filepath.Join(out, "m/index.html"))
	p, _ := os.ReadFile(filepath.Join(out, "p/index.html"))
	if !strings.Contains(string(m), "katex.min.js") || !strings.Contains(string(m), "mermaid.min.js") {
		t.Error("math/mermaid page lacks scripts")
	}
	if strings.Contains(string(p), "katex") || strings.Contains(string(p), "mermaid") {
		t.Error("plain page includes vendor scripts")
	}
}

func TestTemplateOverride(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "layout.html"), []byte(`CUSTOM {{.Title}}`), 0o644)
	out := t.TempDir()
	cfg := &config.Config{Output: out, HTML: config.HTML{TemplateDir: dir}}
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := tg.WritePage(&target.Site{Config: cfg, Tree: &target.FolderNode{}}, &render.Page{Slug: "x", Title: "Hi"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "x/index.html")); string(b) != "CUSTOM Hi" {
		t.Errorf("got %q", b)
	}
}

func TestTitleNotRepeatedWhenPageTitleIsSiteTitle(t *testing.T) {
	out := t.TempDir()
	cfg := &config.Config{Output: out, Search: "none", Site: config.Site{Title: "Local Politics"}}
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	site := &target.Site{Config: cfg, Tree: &target.FolderNode{}}
	for _, p := range []*render.Page{{Slug: "", URL: "/", Title: "Local Politics"}, {Slug: "a", URL: "/a/", Title: "A"}} {
		if err := tg.WritePage(site, p); err != nil {
			t.Fatal(err)
		}
	}
	home, _ := os.ReadFile(filepath.Join(out, "index.html"))
	a, _ := os.ReadFile(filepath.Join(out, "a/index.html"))
	if !strings.Contains(string(home), "<title>Local Politics</title>") || !strings.Contains(string(a), "<title>A · Local Politics</title>") {
		t.Errorf("titles: %q / %q", grepTitle(home), grepTitle(a))
	}
}

func grepTitle(b []byte) string {
	s := string(b)
	i, j := strings.Index(s, "<title>"), strings.Index(s, "</title>")
	if i < 0 || j < i {
		return ""
	}
	return s[i : j+8]
}
