package render

import (
	"regexp"
	"strings"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/dataview"
)

// withDataview evaluates every query in the published pages with the real engine.
func withDataview(t *testing.T, r *Renderer) {
	t.Helper()
	node, err := dataview.FindNode()
	if err != nil {
		t.Skip(err)
	}
	settings, err := dataview.LoadSettings(r.Ix.Vault.Root)
	if err != nil {
		t.Fatal(err)
	}
	reqs, err := r.CollectQueries()
	if err != nil {
		t.Fatal(err)
	}
	res, err := dataview.NewEngine(node, settings, r.Ix).RunIndex(reqs)
	if err != nil {
		t.Fatal(err)
	}
	r.SetDataview(res, settings)
}

func TestCurrentIssuesRendered(t *testing.T) {
	r, warns := fixtureRenderer(t)
	withDataview(t, r)
	p, err := r.Render("Town/Current Issues.md")
	if err != nil {
		t.Fatal(err)
	}
	h := p.HTML
	if n := strings.Count(h, "<table"); n != 4 {
		t.Fatalf("%d tables:\n%s", n, h)
	}
	if !strings.Contains(h, "<th>Issue<span class=\"dataview small-text\">2</span></th><th>Board</th><th>Last updated</th><th>People</th>") {
		t.Errorf("headers:\n%s", h)
	}
	if !strings.Contains(h, "<td>2026-09-30</td>") || !strings.Contains(h, `<a class="internal-link" href="/c/issues/road-repair/">Road Repair</a>`) {
		t.Errorf("cells:\n%s", h)
	}
	if !regexp.MustCompile(`<ul class="dataview-list"><li><a class="internal-link" href="/c/issues/caf%C3%A9-permit/">Café Permit</a></li><li><a class="internal-link" href="/c/issues/road-repair/">Road Repair</a></li></ul>`).MatchString(h) {
		t.Errorf("grouped list:\n%s", h)
	}
	if strings.Contains(h, "people/alice") || !strings.Contains(h, "<td>Alice Smith</td>") {
		t.Errorf("excluded person should be plain text:\n%s", h)
	}
	if !strings.Contains(h, "<td>-</td>") {
		t.Errorf("null should render as '-':\n%s", h)
	}
	if len(*warns) != 0 {
		t.Errorf("warnings %v", *warns)
	}
}

func TestInlineAndUnsupportedAndErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Town/a.md", "---\nstatus: open\n---\nStatus is `= this.status`. JS `$= dv.current()`\n\n```dataviewjs\ndv.list([1])\n```\n\n```dataview\nTABLE nope( FROM\n```\n\n```dataview\nLIST FROM \"Nowhere\"\n```\n\n```dataview\nTASK FROM \"Town\"\n```\n\n- [ ] do [[b]]\n  - [x] sub\n")
	writeFile(t, dir, "Town/b.md", "b")
	r, warns := newTestRenderer(t, dir, ModeHTML)
	withDataview(t, r)
	p, err := r.Render("Town/a.md")
	if err != nil {
		t.Fatal(err)
	}
	h := p.HTML
	for _, want := range []string{"Status is open.", `<span class="dv-unsupported">`, `<div class="dv-unsupported">`, `<div class="dv-error">`,
		"No results to show for list query.", `<input type="checkbox" disabled>`, `<input type="checkbox" disabled checked>`, `href="/c/b/"`} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in\n%s", want, h)
		}
	}
	if len(*warns) != 3 {
		t.Errorf("want 3 warnings (dataviewjs block, $= inline, query error), got %v", *warns)
	}
}

func TestDataviewDisabled(t *testing.T) {
	r, warns := fixtureRenderer(t)
	r.DisableDataview()
	p, err := r.Render("Town/Current Issues.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(p.HTML, "dv-unsupported") != 4 || len(*warns) != 0 {
		t.Errorf("html %s warns %v", p.HTML, *warns)
	}
}
