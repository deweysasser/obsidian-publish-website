package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageEmbeds(t *testing.T) {
	r, warns := fixtureRenderer(t)
	got := pre(t, r, "![[photo.png|200]] ![[photo.png|20x10]] ![alt](img/photo.png)")
	want := `<img src="/c/assets/img/photo.png" alt="photo.png" width="200"> <img src="/c/assets/img/photo.png" alt="photo.png" width="20" height="10"> <img src="/c/assets/img/photo.png" alt="alt">`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if r.Pub.Assets["Town/img/photo.png"] != "img/photo.png" {
		t.Errorf("asset not marked")
	}
	if got := pre(t, r, "![[headshot.png]]"); got != "" || len(*warns) != 1 {
		t.Errorf("excluded image: %q warns %v", got, *warns)
	}
}

func TestMediaEmbeds(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"Town/Town.md", "Town/a.pdf", "Town/a.mp3", "Town/a.mp4"} {
		os.MkdirAll(filepath.Join(dir, filepath.Dir(f)), 0o755)
		os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644)
	}
	r, _ := newTestRenderer(t, dir, ModeHTML)
	got := pre(t, r, "![[a.pdf]]\n![[a.mp3]]\n![[a.mp4]]")
	want := "<iframe class=\"embed-pdf\" src=\"/c/assets/a.pdf\"></iframe>\n<audio controls src=\"/c/assets/a.mp3\"></audio>\n<video controls src=\"/c/assets/a.mp4\"></video>"
	if got != want {
		t.Errorf("got\n%s", got)
	}
}

func TestNoteEmbeds(t *testing.T) {
	r, _ := fixtureRenderer(t)
	got := pre(t, r, "![[2026-08-19 - Meeting#Context]]")
	if !strings.Contains(got, `<div class="embed" data-src="/c/videos/2026-08-19-meeting/">`) ||
		!strings.Contains(got, "## Context") || !strings.Contains(got, "roads.") || strings.Contains(got, "Actions") || strings.Contains(got, "Discussed") {
		t.Errorf("heading embed:\n%s", got)
	}
	got = pre(t, r, "![[2026-08-19 - Meeting#^blk1]]")
	if !strings.Contains(got, "The board met") || strings.Contains(got, "## Context") || strings.Contains(got, "follow up") {
		t.Errorf("block embed:\n%s", got)
	}
	got = pre(t, r, "![[2026-08-19 - Meeting]]")
	// links inside embedded content resolve relative to the embedded note
	if !strings.Contains(got, "[Road Repair > Context](/c/issues/road-repair/#context)") || !strings.Contains(got, "follow up") {
		t.Errorf("whole embed:\n%s", got)
	}
}

func TestEmbedCycleAndExcluded(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "Town"), 0o755)
	os.WriteFile(filepath.Join(dir, "Town/A.md"), []byte("A says ![[B]]"), 0o644)
	os.WriteFile(filepath.Join(dir, "Town/B.md"), []byte("B says ![[A]]"), 0o644)
	r, _ := newTestRenderer(t, dir, ModeHTML)
	got, err := r.Preprocess([]byte("![[A]]"), 1, Context{Path: "Town/B.md", Stack: []string{"Town/B.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "A says") || !strings.Contains(got, "Embed depth exceeded") {
		t.Errorf("cycle:\n%s", got)
	}

	r, _ = fixtureRenderer(t)
	got = pre(t, r, "![[Alice Smith]]")
	if !strings.Contains(got, "Not published") || strings.Contains(got, "Clerk") || strings.Contains(got, "alice") {
		t.Errorf("excluded embed:\n%s", got)
	}
}

func TestEmbedSectionInsideComment(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Town/N.md", "visible\n%%\n## Secret\nhidden text ^blk\n%%\n## Open\nshown\n")
	r, _ := newTestRenderer(t, dir, ModeHTML)
	for _, src := range []string{"![[N#Secret]]", "![[N#^blk]]", "![[N]]", "![[N#Open]]"} {
		if got := pre(t, r, src); strings.Contains(got, "hidden text") {
			t.Errorf("%s leaked commented content: %q", src, got)
		}
	}
	if got := pre(t, r, "![[N#Open]]"); !strings.Contains(got, "shown") {
		t.Errorf("open section missing: %q", got)
	}
}
