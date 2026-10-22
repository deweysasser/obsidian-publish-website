package html

import (
	"os"
	"path"
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

func navTree() *target.FolderNode {
	pg := func(slug string) *render.Page {
		return &render.Page{Slug: slug, URL: "/" + slug + "/", Title: path.Base(slug)}
	}
	b := &target.FolderNode{Name: "B", Folder: "R/A/B", Slug: "a/b", URL: "/a/b/", Pages: []*render.Page{pg("a/b/b1")}}
	a := &target.FolderNode{Name: "A", Folder: "R/A", Slug: "a", URL: "/a/", Folders: []*target.FolderNode{b},
		Pages: []*render.Page{pg("a/a1"), pg("a/a2")}}
	c := &target.FolderNode{Name: "C", Folder: "R/C", Slug: "c", URL: "/c/", Pages: []*render.Page{pg("c/c1")}}
	return &target.FolderNode{Folder: "R", URL: "/", Folders: []*target.FolderNode{a, c}, Pages: []*render.Page{pg("top")}}
}

func TestNavExpandsOnlyThePathToTheCurrentPage(t *testing.T) {
	root := navTree()
	for _, tc := range []struct {
		current   string
		want, not []string
	}{
		{"/a/b/b1/", []string{"/a/", "/a/b/", "/a/b/b1/", "/a/a1/", "/a/a2/", "/c/", "/top/"}, []string{"/c/c1/"}},
		{"/a/", []string{"/a/", "/a/b/", "/a/a1/", "/c/", "/top/"}, []string{"/a/b/b1/", "/c/c1/"}},
		{"/", []string{"/a/", "/c/", "/top/"}, []string{"/a/a1/", "/a/b/", "/c/c1/"}},
	} {
		nav := string(newNav(root).html(tc.current))
		for _, u := range tc.want {
			if !strings.Contains(nav, `href="`+u+`"`) {
				t.Errorf("%s: nav lacks %s:\n%s", tc.current, u, nav)
			}
		}
		for _, u := range tc.not {
			if strings.Contains(nav, `href="`+u+`"`) {
				t.Errorf("%s: nav should not list %s:\n%s", tc.current, u, nav)
			}
		}
		if tc.current != "/" && !strings.Contains(nav, `href="`+tc.current+`" aria-current="page"`) {
			t.Errorf("%s: current page not marked", tc.current)
		}
	}
}

func TestFinalizeWritesRobotsAndNoAllPage(t *testing.T) {
	out := t.TempDir()
	cfg := &config.Config{Output: out, Search: "none", Site: config.Site{Title: "S", BaseURL: "https://x.io/sub/"}}
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := tg.Finalize(&target.Site{Config: cfg, Tree: navTree(), Tags: map[string][]*render.Page{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "all")); err == nil {
		t.Error("/all/ page still written")
	}
	robots, _ := os.ReadFile(filepath.Join(out, "robots.txt"))
	if !strings.Contains(string(robots), "Sitemap: https://x.io/sub/sitemap.xml") {
		t.Errorf("robots.txt: %q", robots)
	}
}

func TestPageMetadata(t *testing.T) {
	out := t.TempDir()
	cfg := &config.Config{Output: out, Search: "none", Site: config.Site{Title: "Site", BaseURL: "https://x.io/sub/"}}
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	site := &target.Site{Config: cfg, Tree: &target.FolderNode{}}
	body := `<p><em>Based on auto-captions; may be approximate.</em></p><p>Short.</p>` +
		`<p><strong>Time on topic</strong> (from transcript timestamps; approximate, includes side talk):</p>` +
		`<p>The council debated the <a href="/x/">camping ordinance</a> for two hours &amp; voted to send it back to committee for revisions.</p>` +
		`<p><span class="yt" data-yt="abcdefghijk"><a href="u"><img src="t"></a></span></p>`
	pages := []*render.Page{
		{Slug: "a", URL: "/sub/a/", Title: "Meeting \"A\"", HTML: body},
		{Slug: "b", URL: "/sub/b/", Title: "B", HTML: body, Frontmatter: map[string]any{"description": "Given description."}},
	}
	for _, p := range pages {
		if err := tg.WritePage(site, p); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := os.ReadFile(filepath.Join(out, "a/index.html"))
	b, _ := os.ReadFile(filepath.Join(out, "b/index.html"))
	for _, want := range []string{
		`<meta name="description" content="The council debated the camping ordinance for two hours &amp; voted to send it back to committee for revisions.">`,
		`<link rel="canonical" href="https://x.io/sub/a/">`,
		`<meta property="og:title" content="Meeting &#34;A&#34;">`,
		`<meta property="og:site_name" content="Site">`,
		`<meta property="og:url" content="https://x.io/sub/a/">`,
		`<meta property="og:image" content="https://i.ytimg.com/vi/abcdefghijk/hqdefault.jpg">`,
		`<meta name="twitter:card" content="summary_large_image">`,
		`<link rel="icon" href="/sub/assets/favicon.svg">`,
	} {
		if !strings.Contains(string(a), want) {
			t.Errorf("missing %s in\n%.1500s", want, a)
		}
	}
	if !strings.Contains(string(b), `<meta name="description" content="Given description.">`) {
		t.Error("frontmatter description not used")
	}
}

func TestDescriptionTruncatesAtWord(t *testing.T) {
	long := "<p>" + strings.Repeat("word ", 60) + "</p>"
	d := describe(&render.Page{HTML: long})
	if len(d) > 165 || !strings.HasSuffix(d, "word…") {
		t.Errorf("%d %q", len(d), d)
	}
}

func TestConfiguredFavicon(t *testing.T) {
	vault, out := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(vault, "Icon.PNG"), []byte("png"), 0o644)
	cfg := &config.Config{Vault: vault, Output: out, Search: "none", Site: config.Site{Title: "S", Favicon: "Icon.PNG"}}
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	site := &target.Site{Config: cfg, Tree: navTree(), Tags: map[string][]*render.Page{}}
	if err := tg.Finalize(site); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "favicon.png")); string(b) != "png" {
		t.Error("favicon not copied")
	}
	if idx, _ := os.ReadFile(filepath.Join(out, "a/index.html")); !strings.Contains(string(idx), `<link rel="icon" href="/favicon.png">`) {
		t.Error("favicon link missing")
	}
}
