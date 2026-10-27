package render

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func usmapRenderer(t *testing.T) (*Renderer, *[]warning) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "Town/Town.md", "home")
	writeFile(t, dir, "Town/US/Massachusetts/Massachusetts.md", "ma")
	writeFile(t, dir, "Town/US/Massachusetts/Boston.md", "b")
	writeFile(t, dir, "Town/US/Rhode Island/Providence.md", "p") // folder without a folder note
	writeFile(t, dir, "Town/US/Texas.md", "a note, not a folder")
	writeFile(t, dir, "Town/US/U.S. Virgin Islands/x.md", "x")
	writeFile(t, dir, "Town/US/Ohio/Ohio.md", "---\npublish: false\n---\n")
	return newTestRenderer(t, dir, ModeHTML)
}

// region returns the markup for one state, from its opening <a> or <g> to
// the matching close.
func region(t *testing.T, out, name string) string {
	t.Helper()
	m := regexp.MustCompile(`<(a|g)\b[^>]*><title>` + regexp.QuoteMeta(name) + `</title>.*?</(a|g)>`).FindString(out)
	if m == "" {
		t.Fatalf("no region %q in output", name)
	}
	return m
}

func TestUSMapLinksPublishedStates(t *testing.T) {
	r, warns := usmapRenderer(t)
	out := pre(t, r, "```usmap\nUS\n```\n")
	if !strings.Contains(out, `<figure class="usmap">`) || !strings.Contains(out, `<svg viewBox="0 0 960 600"`) {
		t.Fatalf("no map:\n%.300s", out)
	}
	for name, href := range map[string]string{
		"Massachusetts":       "/c/us/massachusetts/",
		"Rhode Island":        "/c/us/rhode-island/",
		"Texas":               "/c/us/texas/",
		"U.S. Virgin Islands": "/c/us/u-s-virgin-islands/",
	} {
		if reg := region(t, out, name); !strings.HasPrefix(reg, `<a href="`+href+`"`) {
			t.Errorf("%s: want link to %s, got %.120s", name, href, reg)
		}
	}
	for _, name := range []string{"Ohio", "Wyoming", "Guam"} {
		if reg := region(t, out, name); !strings.HasPrefix(reg, `<g class="off">`) {
			t.Errorf("%s should be unlinked: %.120s", name, reg)
		}
	}
	if n := strings.Count(out, "<title>"); n != 56 {
		t.Errorf("%d regions, want 56 (50 states, DC, 5 territories)", n)
	}
	if reg := region(t, out, "Rhode Island"); !strings.Contains(reg, "<rect") || !strings.Contains(reg, ">RI</text>") {
		t.Errorf("small state needs a labelled square: %.300s", reg)
	}
	if fig := out[strings.Index(out, "<figure"):strings.Index(out, "</figure>")]; strings.Contains(fig, "\n\n") {
		t.Error("blank line inside the map would end the raw HTML block")
	}
	if len(*warns) != 0 {
		t.Errorf("warnings: %v", *warns)
	}
}

func TestUSMapRendersAsHTML(t *testing.T) {
	r, _ := usmapRenderer(t)
	out, _, err := r.MarkdownToHTML(pre(t, r, "Intro.\n\n```usmap\nUS\n```\nAfter.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `<a href="/c/us/massachusetts/"`) || strings.Contains(out, "&lt;svg") || strings.Contains(out, "<pre") || !strings.Contains(out, "<p>After.</p>") {
		t.Errorf("map not passed through as HTML:\n%.400s", out)
	}
}

func TestUSMapFolderResolution(t *testing.T) {
	r, warns := usmapRenderer(t)
	// Relative to the note's folder, or to the published root.
	for _, arg := range []string{"US", "Town/US"} {
		if out := pre(t, r, "```usmap\n"+arg+"\n```\n"); !strings.Contains(out, `href="/c/us/massachusetts/"`) {
			t.Errorf("%q did not resolve", arg)
		}
	}
	out, err := r.Preprocess([]byte("```usmap\nUS\n```\n"), 1, Context{Path: "Town/US/Massachusetts/Boston.md"})
	if err != nil || !strings.Contains(out, `href="/c/us/massachusetts/"`) {
		t.Errorf("root-relative fallback from a nested note failed: %v", err)
	}
	*warns = nil
	out = pre(t, r, "```usmap\nNowhere\n```\n")
	if strings.Contains(out, "<a ") || len(*warns) != 1 || !strings.Contains((*warns)[0].msg, "Nowhere") {
		t.Errorf("missing folder should warn and link nothing: %v", *warns)
	}
}

func stateMapRenderer(t *testing.T) (*Renderer, *[]warning) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "Town/Town.md", "home")
	writeFile(t, dir, "Town/US/Massachusetts/Massachusetts.md", "```statemap\n```\n")
	writeFile(t, dir, "Town/US/Massachusetts/Boston/Boston.md", "b")
	writeFile(t, dir, "Town/US/Massachusetts/Worcester.md", "a town note without a folder")
	writeFile(t, dir, "Town/US/Massachusetts/Smallville/Smallville.md", "---\ncoordinates: [42.1, -72.6]\n---\n")
	writeFile(t, dir, "Town/US/Massachusetts/Atlantis/x.md", "x")
	writeFile(t, dir, "Town/US/Rhode Island/Providence/Providence.md", "p")
	return newTestRenderer(t, dir, ModeHTML)
}

func TestStateMapDotsTowns(t *testing.T) {
	r, warns := stateMapRenderer(t)
	out, err := r.Preprocess([]byte("```statemap\n```\n"), 1, Context{Path: "Town/US/Massachusetts/Massachusetts.md"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `<figure class="statemap">`) {
		t.Fatalf("no state map:\n%.300s", out)
	}
	for _, town := range []string{"boston/", "worcester/", "smallville/"} {
		if !regexp.MustCompile(`<a href="/c/us/massachusetts/` + town + `"><title>[^<]+</title><circle `).MatchString(out) {
			t.Errorf("no dot for %s", town)
		}
	}
	if !strings.Contains(out, `<path class="st"`) || !strings.Contains(out, `<a href="/c/us/rhode-island/"><title>Rhode Island</title><path class="nb"`) {
		t.Error("state outline or linked neighbour missing")
	}
	if strings.Contains(out, "<title>Wyoming</title>") {
		t.Error("far-away states should be left out")
	}
	if fig := out[strings.Index(out, "<figure"):strings.Index(out, "</figure>")]; strings.Contains(fig, "\n\n") {
		t.Error("blank line inside the map would end the raw HTML block")
	}
	if len(*warns) != 1 || !strings.Contains((*warns)[0].msg, "Atlantis") {
		t.Errorf("want one warning about the unknown town, got %v", *warns)
	}
}

func TestStateMapCoordinatesOverride(t *testing.T) {
	r, _ := stateMapRenderer(t)
	out := pre(t, r, "```statemap\nUS/Massachusetts\n```\n")
	m := regexp.MustCompile(`<title>Smallville</title><circle cx="([\d.]+)" cy="([\d.]+)"`).FindStringSubmatch(out)
	b := regexp.MustCompile(`<title>Boston</title><circle cx="([\d.]+)" cy="([\d.]+)"`).FindStringSubmatch(out)
	if m == nil || b == nil {
		t.Fatalf("dots missing:\n%.600s", out)
	}
	// Smallville's override (western MA) puts it well west of Boston.
	if sx, _ := strconv.ParseFloat(m[1], 64); sx >= mustFloat(b[1])-10 {
		t.Errorf("override ignored: Smallville x %s, Boston x %s", m[1], b[1])
	}
}

func mustFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func TestUSMapShowsTownDots(t *testing.T) {
	r, _ := stateMapRenderer(t)
	out := pre(t, r, "```usmap\nUS\n```\n")
	g := regexp.MustCompile(`<g class="towns" aria-hidden="true">(.*?)</g>`).FindStringSubmatch(out)
	if g == nil {
		t.Fatalf("no town layer:\n%.300s", out[len(out)-600:])
	}
	// Boston, Worcester, Smallville (override), Providence; Atlantis can't be placed.
	if n := strings.Count(g[1], "<circle "); n != 4 {
		t.Errorf("%d town dots, want 4", n)
	}
	if strings.Contains(g[1], "<a ") || strings.Contains(g[1], "<title>") {
		t.Error("town dots should be decoration only")
	}
	for i := 0; i < 20; i++ { // map iteration order must not leak into the output
		if again := pre(t, r, "```usmap\nUS\n```\n"); again != out {
			t.Fatal("town dots differ between renders")
		}
	}
}
