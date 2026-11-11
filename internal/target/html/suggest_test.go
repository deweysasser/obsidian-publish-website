package html

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/deweysasser/obsidian-publish-website/internal/render"
	"github.com/deweysasser/obsidian-publish-website/internal/target"
)

// suggestSite builds a small site with a nested page and the given
// site.suggest, through Finalize, and returns the output directory.
func suggestSite(t *testing.T, sg *config.Suggest) string {
	t.Helper()
	out := t.TempDir()
	cfg := &config.Config{Output: out, Root: "R", Target: "html", Search: "none",
		Site: config.Site{Title: "S", BaseURL: "https://example.org/", Suggest: sg}}
	cfg.ApplyDefaults() // validation is config's business; see its tests
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	nested := &render.Page{Slug: "us/ma/café", URL: "/us/ma/caf%C3%A9/", Title: "Café", SourcePath: "R/us/ma/café.md", HTML: "<p>x</p>"}
	site := &target.Site{Config: cfg, Tree: &target.FolderNode{}, Pages: []*render.Page{nested}}
	if err := tg.WritePage(site, nested); err != nil {
		t.Fatal(err)
	}
	if err := tg.Finalize(site); err != nil {
		t.Fatal(err)
	}
	return out
}

func readOut(t *testing.T, out, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const nestedPage = "%2Fus%2Fma%2Fcaf%25C3%25A9%2F" // the page's URL path, query-encoded

func TestSuggestDefaultKind(t *testing.T) {
	out := suggestSite(t, &config.Suggest{Endpoint: "/api/suggest"})

	page := readOut(t, out, "us/ma/café/index.html")
	want := `<p class="suggest-links"><a href="/suggest/?kind=correction&amp;page=` + nestedPage + `">Suggest a change</a></p>`
	if !strings.Contains(page, want) {
		t.Errorf("nested page lacks %s", want)
	}

	form := readOut(t, out, "suggest/index.html")
	for _, want := range []string{
		`<meta name="robots" content="noindex">`,
		`<form class="suggest-form" data-endpoint="/api/suggest" data-pagefind-ignore="all">`,
		`<noscript>`,
		`<input type="hidden" name="kind" value="correction">`,
		`<textarea id="suggest-correction-message" name="message" maxlength="5000" rows="6" required></textarea>`,
		`<input type="text" id="suggest-page" name="page" maxlength="300" readonly>`,
		`<input type="text" id="suggest-contact" name="contact" maxlength="200" autocomplete="email">`,
		"Only if you'd like a reply. It's never published.",
		`<div class="suggest-turnstile"></div>`,
		`/assets/_v/suggest.`, // its own script and style
	} {
		if !strings.Contains(form, want) {
			t.Errorf("form page lacks %q", want)
		}
	}
	// no kind selector, and no inputs besides kind, the message, page and contact
	if strings.Contains(form, `type="radio"`) {
		t.Error("a kind selector with one kind")
	}
	if n := strings.Count(form, "<input"); n != 3 {
		t.Errorf("%d inputs, want kind, page and contact", n)
	}
	if strings.Contains(form, "data-pagefind-body") || strings.Contains(form, "suggest-links") {
		t.Error("the form page is indexed for search, or links to itself")
	}

	if strings.Contains(readOut(t, out, "404.html"), "suggest-links") {
		t.Error("404 page has suggest links")
	}
	if strings.Contains(readOut(t, out, "sitemap.xml"), "/suggest/") {
		t.Error("the form page is in the sitemap")
	}
	if strings.Contains(page, "suggest.js") || strings.Contains(page, "suggest.css") {
		t.Error("an ordinary page loads the form's assets")
	}
}

// siteKinds is a two-kind config like the local-government site's (test
// fixture: site-specific words belong here, not in the generator).
func siteKinds() *config.Suggest {
	return &config.Suggest{Endpoint: "/api/suggest", Kinds: []config.SuggestKind{
		{ID: "correction", Link: "Suggest a change", Title: "Suggest a change to this page", Message: "required"},
		{ID: "town", Link: "Request a town", Title: "Request a town", Message: "optional", MessageLabel: "Anything we should know?",
			Fields: []config.SuggestField{
				{Name: "town", Label: "Town", Required: true, MaxLength: 100},
				{Name: "state", Label: "State", Required: true, MaxLength: 60},
			}},
	}}
}

func TestSuggestTwoKinds(t *testing.T) {
	out := suggestSite(t, siteKinds())

	page := readOut(t, out, "us/ma/café/index.html")
	want := `<p class="suggest-links"><a href="/suggest/?kind=correction&amp;page=` + nestedPage + `">Suggest a change</a> · ` +
		`<a href="/suggest/?kind=town&amp;page=` + nestedPage + `">Request a town</a></p>`
	if !strings.Contains(page, want) {
		t.Errorf("nested page lacks both links in order: %s", want)
	}

	form := readOut(t, out, "suggest/index.html")
	for _, want := range []string{
		`<h1 class="page-title">Suggestions</h1>`,
		`<input type="radio" name="kind" value="correction" checked> Suggest a change</label>`,
		`<input type="radio" name="kind" value="town"> Request a town</label>`,
		`<div class="suggest-kind" data-kind="correction"><h2>Suggest a change to this page</h2>`,
		// the second kind starts hidden and disabled until suggest.js picks it
		`<div class="suggest-kind" data-kind="town" hidden><h2>Request a town</h2>`,
		`<input type="text" id="suggest-town-town" name="town" maxlength="100" required disabled>`,
		`<input type="text" id="suggest-town-state" name="state" maxlength="60" required disabled>`,
		`<label for="suggest-town-message">Anything we should know? <span class="hint-inline">(optional)</span></label>` +
			`<textarea id="suggest-town-message" name="message" maxlength="5000" rows="6" disabled></textarea>`,
	} {
		if !strings.Contains(form, want) {
			t.Errorf("form lacks %q", want)
		}
	}
}

func TestSuggestReservesItsSlug(t *testing.T) {
	cfg := &config.Config{Output: t.TempDir(), Search: "none", Site: config.Site{Title: "S", Suggest: &config.Suggest{Endpoint: "/api/suggest"}}}
	cfg.ApplyDefaults()
	tg, _ := New(cfg)
	site := &target.Site{Config: cfg, Tree: &target.FolderNode{}}
	err := tg.WritePage(site, &render.Page{Slug: "suggest/x", URL: "/suggest/x/", SourcePath: "R/suggest/x.md"})
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("err %v", err)
	}
	cfg.Site.Suggest = nil
	tg, _ = New(cfg)
	if err := tg.WritePage(site, &render.Page{Slug: "suggest/x", URL: "/suggest/x/"}); err != nil {
		t.Errorf("without site.suggest: %v", err)
	}
}

func TestSuggestDisabledPublishesNothing(t *testing.T) {
	out := suggestSite(t, nil)
	if _, err := os.Stat(filepath.Join(out, "suggest")); err == nil {
		t.Error("/suggest/ published without site.suggest")
	}
	matches, _ := filepath.Glob(filepath.Join(out, "assets", "*suggest*"))
	v, _ := filepath.Glob(filepath.Join(out, "assets", "_v", "*suggest*"))
	if len(matches)+len(v) > 0 {
		t.Errorf("suggest assets published: %v %v", matches, v)
	}
	if strings.Contains(readOut(t, out, "us/ma/café/index.html"), "suggest") {
		t.Error("a page mentions suggest without site.suggest")
	}
}

// TestSuggestHasNoSiteWords keeps the feature generic: kinds, labels and
// fields come from each site's config, so the generator's own code for it
// must not carry one site's domain words.
func TestSuggestHasNoSiteWords(t *testing.T) {
	words := regexp.MustCompile(`(?i)\btowns?\b|\bstates?\b`)
	files := map[string]func() ([]byte, error){
		"assets/suggest.js":     func() ([]byte, error) { return assetFS.ReadFile("assets/suggest.js") },
		"assets/suggest.css":    func() ([]byte, error) { return assetFS.ReadFile("assets/suggest.css") },
		"templates/layout.html": func() ([]byte, error) { return templateFS.ReadFile("templates/layout.html") },
		"suggest.go":            func() ([]byte, error) { return os.ReadFile("suggest.go") },
		"html.go":               func() ([]byte, error) { return os.ReadFile("html.go") },
		"web/src/suggest.ts":    func() ([]byte, error) { return os.ReadFile("../../../web/src/suggest.ts") },
	}
	for name, read := range files {
		b, err := read()
		if err != nil {
			t.Fatal(err)
		}
		if m := words.Find(b); m != nil {
			t.Errorf("%s contains the site-specific word %q", name, m)
		}
	}
}

// TestSuggestFormFixture keeps web/test/fixtures/suggest-form.html, the
// markup the TypeScript tests drive suggest.js against, identical to what
// suggestForm renders for siteKinds. Regenerate with
// UPDATE_FIXTURES=1 go test ./internal/target/html/ -run SuggestFormFixture.
func TestSuggestFormFixture(t *testing.T) {
	sg := siteKinds()
	cfg := &config.Config{Site: config.Site{Suggest: sg}}
	cfg.ApplyDefaults()
	got := "<!-- Generated by TestSuggestFormFixture (internal/target/html); do not edit. -->\n" + string(suggestForm(sg)) + "\n"
	path := filepath.Join("..", "..", "..", "web", "test", "fixtures", "suggest-form.html")
	if os.Getenv("UPDATE_FIXTURES") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path) // #nosec G304 -- a fixed path in the repo
	if err != nil {
		t.Fatalf("%v (regenerate with UPDATE_FIXTURES=1)", err)
	}
	if string(want) != got {
		t.Errorf("%s is out of date; regenerate with UPDATE_FIXTURES=1 go test ./internal/target/html/ -run SuggestFormFixture", path)
	}
}
