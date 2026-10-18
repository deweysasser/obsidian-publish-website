package jekyll_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/build"
	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/rs/zerolog"
)

func buildFixture(t *testing.T, out, baseURL string) {
	t.Helper()
	cfg := &config.Config{Vault: "../../../testdata/fixture-vault", Root: "Town", Exclude: []string{"People/**"},
		Target: "jekyll", Output: out, Site: config.Site{Title: "T", BaseURL: baseURL}}
	cfg.ExportFrontmatter = true
	cfg.ApplyDefaults()
	if _, err := build.Run(cfg, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestJekyllExport(t *testing.T) {
	out := t.TempDir()
	buildFixture(t, out, "https://x.io/blog/")
	for _, f := range []string{"_town/index.md", "_town/issues/index.md", "_town/issues/road-repair.md", "assets/town/img/photo.png", "_config.obsidian-publish.yml"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	rr := read(t, filepath.Join(out, "_town/issues/road-repair.md"))
	for _, want := range []string{"permalink: /town/issues/road-repair/\n", "layout: page\n", "{% raw %}", "{% endraw %}",
		"[2026-08-19 - Meeting](/blog/town/videos/2026-08-19-meeting/)", `<img src="/blog/assets/town/img/photo.png"`, "last_updated: \"2026-09-22\""} {
		if !strings.Contains(rr, want) {
			t.Errorf("missing %q in\n%s", want, rr)
		}
	}
	if home := read(t, filepath.Join(out, "_town/index.md")); !strings.Contains(home, `<span class="tag">#civic</span>`) {
		t.Errorf("tags should render unlinked for jekyll:\n%s", home)
	}
	if cfg := read(t, filepath.Join(out, "_config.obsidian-publish.yml")); !strings.Contains(cfg, "town:\n") || !strings.Contains(cfg, "output: true") {
		t.Errorf("collection config:\n%s", cfg)
	}
}

func TestJekyllRawEscape(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "R"), 0o755)
	os.WriteFile(filepath.Join(dir, "R", "a.md"), []byte("x {{ site.secret }} {% endraw %} y"), 0o644)
	out := t.TempDir()
	cfg := &config.Config{Vault: dir, Root: "R", Target: "jekyll", Output: out}
	cfg.ApplyDefaults()
	if _, err := build.Run(cfg, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(out, "_r/a.md"))
	if !strings.Contains(got, "{% endraw %}{{ '{% endraw %}' }}{% raw %}") || !strings.HasSuffix(got, "{% endraw %}\n") {
		t.Errorf("literal endraw not escaped:\n%s", got)
	}
}

func TestJekyllBuildsSite(t *testing.T) {
	jekyll, err := exec.LookPath("jekyll")
	if err != nil {
		t.Skip("jekyll not on PATH")
	}
	out := t.TempDir()
	os.WriteFile(filepath.Join(out, "_config.yml"), []byte("title: T\nmarkdown: kramdown\n"), 0o644)
	os.MkdirAll(filepath.Join(out, "_layouts"), 0o755)
	os.WriteFile(filepath.Join(out, "_layouts/page.html"), []byte("<h1>{{ page.title }}</h1>{{ content }}"), 0o644)
	buildFixture(t, out, "")
	cmd := exec.Command(jekyll, "build", "--source", out, "--destination", filepath.Join(out, "_site"), "--config",
		filepath.Join(out, "_config.yml")+","+filepath.Join(out, "_config.obsidian-publish.yml"))
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("jekyll: %v\n%s", err, b)
	}
	page := read(t, filepath.Join(out, "_site/town/issues/road-repair/index.html"))
	if !strings.Contains(page, `href="/town/videos/2026-08-19-meeting/"`) {
		t.Errorf("link missing in jekyll output:\n%s", page)
	}
	if ci := read(t, filepath.Join(out, "_site/town/current-issues/index.html")); strings.Count(ci, "<table") != 4 {
		t.Errorf("tables missing in jekyll output:\n%s", ci)
	}
}
