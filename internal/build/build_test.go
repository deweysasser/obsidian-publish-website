package build

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/deweysasser/obsidian-publish-website/internal/dataview"
	"github.com/deweysasser/obsidian-publish-website/internal/youtube"
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
		"issues/index.html", "videos/index.html", "robots.txt", "tags/index.html", "tags/civic-roads/index.html",
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

func TestDataviewSettingsOverrideVault(t *testing.T) {
	requireNode(t)
	out := t.TempDir()
	cfg := fixtureConfig(out, "html")
	cfg.DataviewSettings = map[string]any{"defaultDateFormat": "MMM d"}
	if _, err := Run(cfg, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	ci := read(t, filepath.Join(out, "current-issues/index.html"))
	if !strings.Contains(ci, "Sep 30") || strings.Contains(ci, "2026-09-30") {
		t.Errorf("date format override not applied:\n%s", ci)
	}
}

func TestHiddenTags(t *testing.T) {
	out := t.TempDir()
	cfg := fixtureConfig(out, "html")
	cfg.Site.HiddenTags = []string{"Civic"} // case-insensitive, no '#', hides nested tags too
	if _, err := Run(cfg, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"tags/civic", "tags/civic-roads", "tags/index.html"} {
		if _, err := os.Stat(filepath.Join(out, gone)); err == nil {
			t.Errorf("%s still written", gone)
		}
	}
	rr := read(t, filepath.Join(out, "issues/road-repair/index.html"))
	if strings.Contains(rr, `class="tag"`) || strings.Contains(rr, `/tags/`) {
		t.Error("hidden tag still shown on page or Tags link present with no tags")
	}
	if home := read(t, filepath.Join(out, "index.html")); strings.Contains(home, "#civic") {
		t.Error("inline hidden tag still rendered")
	}
}

func TestTagsOff(t *testing.T) {
	out := t.TempDir()
	cfg := fixtureConfig(out, "html")
	off := false
	cfg.Site.Tags = &off
	if _, err := Run(cfg, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "tags")); err == nil {
		t.Error("tags/ still written")
	}
	for _, f := range []string{"index.html", "issues/road-repair/index.html", "sitemap.xml"} {
		if s := read(t, filepath.Join(out, f)); strings.Contains(s, `class="tag"`) || strings.Contains(s, "/tags/") || strings.Contains(s, "#civic") {
			t.Errorf("%s still shows tags", f)
		}
	}
}

// youtubeVault is a one-note vault linking a YouTube video, with the oEmbed
// endpoint pointed at srv and a private cache.
func youtubeVault(t *testing.T, endpoint string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "R"), 0o755)
	os.WriteFile(filepath.Join(dir, "R", "a.md"), []byte("---\nvideo: https://www.youtube.com/watch?v=abcdefghijk\n---\nA meeting.\n"), 0o644)
	t.Setenv("XDG_CACHE_HOME", t.TempDir()) // os.UserCacheDir on Linux
	old := youtube.Endpoint
	youtube.Endpoint = endpoint
	t.Cleanup(func() { youtube.Endpoint = old })
	cfg := &config.Config{Vault: dir, Root: "R", Output: filepath.Join(t.TempDir(), "o"), Search: "none", Dataview: "none", Site: config.Site{BaseURL: "https://x.io/"}}
	cfg.ApplyDefaults()
	return cfg
}

func TestYouTubeCheckFailureIsABuildWarning(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("relies on XDG_CACHE_HOME for a private cache")
	}
	// A TLS server the build doesn't trust: the failure a container without
	// a CA bundle had on every lookup.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	cfg := youtubeVault(t, srv.URL)
	rep, err := Run(cfg, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], "1 of 1 YouTube videos") || !strings.Contains(rep.Warnings[0], "certificate") {
		t.Errorf("warnings %v", rep.Warnings)
	}
	cfg.Strict = true
	if _, err := Run(cfg, zerolog.Nop()); err == nil || !strings.Contains(err.Error(), "YouTube") {
		t.Errorf("--strict: err %v", err)
	}
}

func TestYouTubeCheckSuccessIsQuiet(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("relies on XDG_CACHE_HOME for a private cache")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(401) }))
	defer srv.Close()
	cfg := youtubeVault(t, srv.URL)
	rep, err := Run(cfg, zerolog.Nop())
	if err != nil || len(rep.Warnings) != 0 {
		t.Errorf("err %v warnings %v", err, rep.Warnings)
	}
}

// A vault note may link to a generated page with a root-relative link, such
// as the suggestion form; it passes through unchanged and isn't a broken link.
func TestRootRelativeLinkToGeneratedPage(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "R"), 0o755)
	os.WriteFile(filepath.Join(dir, "R", "a.md"), []byte("[ask for one](/suggest/?kind=other&page=/a/)\n"), 0o644)
	out := filepath.Join(t.TempDir(), "o")
	cfg := &config.Config{Vault: dir, Root: "R", Output: out, Search: "none", Dataview: "none",
		Site: config.Site{BaseURL: "https://x.io/", Suggest: &config.Suggest{Endpoint: "/api/suggest"}}}
	cfg.ApplyDefaults()
	rep, err := Run(cfg, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Warnings) != 0 {
		t.Errorf("warnings %v", rep.Warnings)
	}
	if page := read(t, filepath.Join(out, "a/index.html")); !strings.Contains(page, `<a href="/suggest/?kind=other&amp;page=/a/">ask for one</a>`) {
		t.Errorf("link changed:\n%s", page)
	}
}
