package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/deweysasser/obsidian-publish-website/internal/dataview"
	"github.com/rs/zerolog"
)

const fixtureVault = "../../testdata/fixture-vault"

func fixtureConfig(out, target string) *config.Config {
	c := &config.Config{Vault: fixtureVault, Root: "Town", Exclude: []string{"People/**"},
		Target: target, Output: out, Search: "none", Site: config.Site{Title: "T", BaseURL: "https://x.io/c/"}}
	if _, err := dataview.FindNode(); err != nil {
		c.Dataview = "none" // keep confidentiality and layout tests running without Node
	}
	c.ApplyDefaults()
	return c
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBuildHTMLFixture(t *testing.T) {
	requireNode(t)
	out := t.TempDir()
	rep, err := Run(fixtureConfig(out, "html"), zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"index.html", "issues/road-repair/index.html", "videos/2026-08-19-meeting/index.html",
		"issues/index.html", "videos/index.html", "all/index.html", "tags/index.html", "tags/civic-roads/index.html",
		"sitemap.xml", "404.html", ".nojekyll", "assets/style.css", "current-issues/index.html", "issues/café-permit/index.html"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "people")); err == nil {
		t.Error("people/ was published")
	}
	rr := read(t, filepath.Join(out, "issues/road-repair/index.html"))
	if !strings.Contains(rr, `href="/c/videos/2026-08-19-meeting/"`) {
		t.Error("meeting link missing")
	}
	if regexp.MustCompile(`<a[^>]*>Alice Smith</a>`).MatchString(rr) || !strings.Contains(rr, "Alice Smith") {
		t.Error("Alice Smith should be plain text")
	}
	if !strings.Contains(rr, "<title>Road Repair · T</title>") || !strings.Contains(rr, `data-pagefind-body`) {
		t.Error("layout missing title or pagefind body")
	}
	sm := read(t, filepath.Join(out, "sitemap.xml"))
	if !strings.Contains(sm, "<loc>https://x.io/c/issues/road-repair/</loc>") {
		t.Errorf("sitemap:\n%s", sm)
	}
	if rep.Pages != 6 {
		t.Errorf("pages %d", rep.Pages)
	}
	ci := read(t, filepath.Join(out, "current-issues/index.html"))
	if strings.Count(ci, "<table") != 4 || !strings.Contains(ci, "2026-09-30") {
		t.Errorf("dataview tables not rendered:\n%s", ci)
	}
	idx := read(t, filepath.Join(out, "index.html"))
	if !strings.Contains(idx, `href="/c/issues/"`) {
		t.Error("home folder index should link to issues")
	}
}

func TestBuildRefusesForeignOutputDir(t *testing.T) {
	out := t.TempDir()
	os.WriteFile(filepath.Join(out, "precious.txt"), []byte("x"), 0o644)
	if _, err := Run(fixtureConfig(out, "html"), zerolog.Nop()); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("want refusal, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "precious.txt")); err != nil {
		t.Error("foreign file deleted")
	}
}

func TestBuildRebuildsOwnOutput(t *testing.T) {
	out := filepath.Join(t.TempDir(), "site")
	cfg := fixtureConfig(out, "html")
	if _, err := Run(cfg, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(out, "stale.html"), []byte("x"), 0o644)
	if _, err := Run(cfg, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "stale.html")); err == nil {
		t.Error("stale file kept")
	}
}

func TestBuildDataviewNone(t *testing.T) {
	out := t.TempDir()
	cfg := fixtureConfig(out, "html")
	cfg.Dataview = "none"
	if _, err := Run(cfg, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if ci := read(t, filepath.Join(out, "current-issues/index.html")); strings.Contains(ci, "<table") || !strings.Contains(ci, "dv-unsupported") {
		t.Error("dataview: none should render placeholders")
	}
}

func TestBuildMissingRoot(t *testing.T) {
	cfg := fixtureConfig(t.TempDir(), "html")
	cfg.Root = "Nope"
	if _, err := Run(cfg, zerolog.Nop()); err == nil {
		t.Fatal("want error")
	}
}

func TestWarningsOnlyForPublishedTree(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "R"), 0o755)
	os.MkdirAll(filepath.Join(dir, "Other"), 0o755)
	os.WriteFile(filepath.Join(dir, "R", "a.md"), []byte("ok"), 0o644)
	os.WriteFile(filepath.Join(dir, "R", "bad.md"), []byte("---\n: : :\n---\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "Other", "bad.md"), []byte("---\n: : :\n---\n"), 0o644)
	cfg := &config.Config{Vault: dir, Root: "R", Output: filepath.Join(t.TempDir(), "o"), Search: "none", Site: config.Site{BaseURL: "https://x.io/"}}
	cfg.ApplyDefaults()
	rep, err := Run(cfg, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Warnings) != 1 || !strings.HasPrefix(rep.Warnings[0], "R/bad.md") {
		t.Errorf("warnings %v", rep.Warnings)
	}
}

func TestBuildWithPagefind(t *testing.T) {
	mode := os.Getenv("OBSIDIAN_PUBLISH_TOOLS_RUNNER")
	if mode == "" {
		t.Skip("set OBSIDIAN_PUBLISH_TOOLS_RUNNER=docker|host to run pagefind")
	}
	out := t.TempDir()
	cfg := fixtureConfig(out, "html")
	cfg.Search = "pagefind"
	cfg.Tools.Runner = mode
	if _, err := Run(cfg, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	entry := read(t, filepath.Join(out, "pagefind/pagefind-entry.json"))
	if !strings.Contains(entry, `"page_count":6`) {
		t.Errorf("pagefind should index exactly the 6 note pages: %s", entry)
	}
	if !strings.Contains(read(t, filepath.Join(out, "index.html")), "pagefind-ui.js") {
		t.Error("search UI not included")
	}
}

func TestBuildPagefindMissingFailsFast(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for runner, want := range map[string]string{"host": "pagefind", "docker": "docker"} {
		out := filepath.Join(t.TempDir(), "o")
		cfg := fixtureConfig(out, "html")
		cfg.Search = "pagefind"
		cfg.Tools.Runner = runner
		if _, err := Run(cfg, zerolog.Nop()); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err %v", runner, err)
		}
		if _, err := os.Stat(out); err == nil {
			t.Errorf("%s: output written despite missing tool", runner)
		}
	}
}

func TestStrictCountsSitemapWarning(t *testing.T) {
	cfg := fixtureConfig(filepath.Join(t.TempDir(), "o"), "html")
	cfg.Site.BaseURL = ""
	cfg.Strict = true
	if _, err := Run(cfg, zerolog.Nop()); err == nil || !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("err %v", err)
	}
}
