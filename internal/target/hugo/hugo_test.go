package hugo_test

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

func buildFixture(t *testing.T, out string) {
	t.Helper()
	cfg := &config.Config{Vault: "../../../testdata/fixture-vault", Root: "Town", Exclude: []string{"People/**"},
		Target: "hugo", Output: out, Site: config.Site{Title: "T", BaseURL: "https://x.io/"}}
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

func TestHugoExport(t *testing.T) {
	out := t.TempDir()
	buildFixture(t, out)
	for _, f := range []string{"content/town/_index.md", "content/town/issues/_index.md", "content/town/issues/road-repair/index.md",
		"content/town/current-issues/index.md", "static/town/assets/img/photo.png"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "content/town/people")); err == nil {
		t.Error("people exported")
	}
	rr := read(t, filepath.Join(out, "content/town/issues/road-repair/index.md"))
	for _, want := range []string{"title: Road Repair\n", "url: /town/issues/road-repair/\n", "tags:\n    - civic-roads\n", "obsidian:\n", "status: open", "last_updated: \"2026-09-22\"",
		"[2026-08-19 - Meeting](/town/videos/2026-08-19-meeting/)"} {
		if !strings.Contains(rr, want) {
			t.Errorf("road repair missing %q:\n%s", want, rr)
		}
	}
	if !strings.HasPrefix(rr, "---\n") {
		t.Error("frontmatter")
	}
	ci := read(t, filepath.Join(out, "content/town/current-issues/index.md"))
	if strings.Count(ci, "<table") != 4 {
		t.Errorf("dataview tables in hugo export:\n%s", ci)
	}
}

func TestHugoShortcodesEscaped(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "R"), 0o755)
	os.WriteFile(filepath.Join(dir, "R", "a.md"), []byte("x {{< youtube 1 >}} y {{% z %}}"), 0o644)
	out := t.TempDir()
	cfg := &config.Config{Vault: dir, Root: "R", Target: "hugo", Output: out}
	cfg.ApplyDefaults()
	if _, err := build.Run(cfg, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(out, "content/r/a/index.md"))
	if strings.Contains(got, "{{< youtube") || !strings.Contains(got, "{{</* youtube 1 */>}}") || !strings.Contains(got, "{{%/* z */%}}") {
		t.Errorf("shortcodes not escaped:\n%s", got)
	}
}

func TestHugoRefusesForeignContent(t *testing.T) {
	out := t.TempDir()
	os.MkdirAll(filepath.Join(out, "content/town"), 0o755)
	os.WriteFile(filepath.Join(out, "content/town/mine.md"), []byte("x"), 0o644)
	cfg := &config.Config{Vault: "../../../testdata/fixture-vault", Root: "Town", Target: "hugo", Output: out}
	cfg.ApplyDefaults()
	if _, err := build.Run(cfg, zerolog.Nop()); err == nil {
		t.Fatal("want refusal")
	}
}

func TestHugoBuildsSite(t *testing.T) {
	hugo, err := exec.LookPath("hugo")
	if err != nil {
		t.Skip("hugo not on PATH")
	}
	out := t.TempDir()
	os.WriteFile(filepath.Join(out, "hugo.toml"), []byte("baseURL = 'https://x.io/'\ntitle = 'T'\n[markup.goldmark.renderer]\nunsafe = true\n"), 0o644)
	os.MkdirAll(filepath.Join(out, "layouts/_default"), 0o755)
	os.WriteFile(filepath.Join(out, "layouts/_default/single.html"), []byte("<h1>{{ .Title }}</h1>{{ .Content }}"), 0o644)
	os.WriteFile(filepath.Join(out, "layouts/_default/list.html"), []byte("<h1>{{ .Title }}</h1>{{ .Content }}{{ range .Pages }}<a href=\"{{ .RelPermalink }}\">{{ .Title }}</a>{{ end }}"), 0o644)
	buildFixture(t, out)
	cmd := exec.Command(hugo, "--source", out, "--quiet")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hugo: %v\n%s", err, b)
	}
	page := read(t, filepath.Join(out, "public/town/issues/road-repair/index.html"))
	if !strings.Contains(page, `href="/town/videos/2026-08-19-meeting/"`) {
		t.Errorf("link missing in hugo output:\n%s", page)
	}
	if ci := read(t, filepath.Join(out, "public/town/current-issues/index.html")); strings.Count(ci, "<table") != 4 {
		t.Errorf("tables missing in hugo output")
	}
}
