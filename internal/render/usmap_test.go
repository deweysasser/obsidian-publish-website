package render

import (
	"regexp"
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
