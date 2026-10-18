package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/rs/zerolog"
)

const fixtureVault = "../../testdata/fixture-vault"

func fixtureConfig(out, target string) *config.Config {
	c := &config.Config{Vault: fixtureVault, Root: "Town", Exclude: []string{"People/**"},
		Target: target, Output: out, Search: "none", Site: config.Site{Title: "T", BaseURL: "https://x.io/c/"}}
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

func TestBuildMissingRoot(t *testing.T) {
	cfg := fixtureConfig(t.TempDir(), "html")
	cfg.Root = "Nope"
	if _, err := Run(cfg, zerolog.Nop()); err == nil {
		t.Fatal("want error")
	}
}
