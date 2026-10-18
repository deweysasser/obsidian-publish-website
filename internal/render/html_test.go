package render

import (
	"strings"
	"testing"
)

func TestMarkdownToHTML(t *testing.T) {
	r, _ := fixtureRenderer(t)
	html, toc, err := r.MarkdownToHTML("## Context\n\ntext[^1]\n\n## Context\n\n### Log & Notes\n\n```mermaid\ngraph TD; A-->B\n```\n\n```go\nx := 1 < 2\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n[^1]: note\n\n<div class=\"x\">\n\n**md inside html**\n\n</div>\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<h2 id="context">Context</h2>`, `<h2 id="context-1">Context</h2>`, `<h3 id="log-notes">Log &amp; Notes</h3>`,
		`<pre class="mermaid">graph TD; A--&gt;B`, `<pre><code class="language-go">x := 1 &lt; 2`,
		`<table>`, `class="footnote-ref"`, `<strong>md inside html</strong>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in\n%s", want, html)
		}
	}
	if len(toc) != 3 || toc[1].ID != "context-1" || toc[2].Level != 3 || toc[2].Text != "Log & Notes" {
		t.Errorf("toc %+v", toc)
	}
}

func TestRenderPageTitleAndBacklinks(t *testing.T) {
	r, _ := fixtureRenderer(t)
	p, err := r.Render("Town/Issues/Road Repair.md")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Road Repair" || p.Slug != "issues/road-repair" || p.URL != "/c/issues/road-repair/" {
		t.Errorf("page %+v", p)
	}
	var back []string
	for _, b := range p.Backlinks {
		back = append(back, b.Path)
	}
	if strings.Join(back, ",") != "Town/Issues/Zoning & Lots.md,Town/Videos/2026-08-19 - Meeting.md" {
		t.Errorf("backlinks %v", back)
	}
	if !strings.Contains(p.HTML, `<a href="/c/videos/2026-08-19-meeting/">2026-08-19 - Meeting</a>`) {
		t.Errorf("html:\n%s", p.HTML)
	}
	if len(p.Tags) != 1 || p.Tags[0] != "#civic/roads" {
		t.Errorf("tags %v", p.Tags)
	}

	m, err := r.Render("Town/Videos/2026-08-19 - Meeting.md")
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "2026-08-19 - Meeting" || strings.Contains(m.HTML, "<h1") {
		t.Errorf("H1 title not lifted: %q\n%s", m.Title, m.HTML)
	}
}

func TestRenderFrontmatterTitle(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Town/x.md", "---\ntitle: Custom\n---\n# Heading\nbody")
	r, _ := newTestRenderer(t, dir, ModeHTML)
	p, err := r.Render("Town/x.md")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Custom" || !strings.Contains(p.HTML, "<h1") {
		t.Errorf("title %q html %s", p.Title, p.HTML)
	}
}

func TestRenderMarkdownModeHasNoHTML(t *testing.T) {
	r, _ := newTestRenderer(t, "../../testdata/fixture-vault", ModeMarkdown)
	p, err := r.Render("Town/Issues/Road Repair.md")
	if err != nil {
		t.Fatal(err)
	}
	if p.HTML != "" || !strings.Contains(p.Markdown, "[2026-08-19 - Meeting](</c/videos/2026-08-19-meeting/>)") {
		t.Errorf("markdown mode page %+v", p)
	}
}
