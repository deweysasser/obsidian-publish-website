package render

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/deweysasser/obsidian-publish-website/internal/index"
	"github.com/deweysasser/obsidian-publish-website/internal/selector"
	"github.com/deweysasser/obsidian-publish-website/internal/urls"
	"github.com/deweysasser/obsidian-publish-website/internal/vault"
)

type warning struct {
	path string
	line int
	msg  string
}

func newTestRenderer(t *testing.T, dir string, mode Mode) (*Renderer, *[]warning) {
	t.Helper()
	v, err := vault.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	ix := index.Build(v)
	pub, err := selector.Select(ix, &config.Config{Root: "Town", Exclude: []string{"People/**"}})
	if err != nil {
		t.Fatal(err)
	}
	var warns []warning
	r := NewRenderer(ix, pub, urls.HTML("/c/"), mode, func(p string, l int, m string) {
		warns = append(warns, warning{p, l, m})
	})
	return r, &warns
}

func fixtureRenderer(t *testing.T) (*Renderer, *[]warning) {
	return newTestRenderer(t, "../../testdata/fixture-vault", ModeHTML)
}

func pre(t *testing.T, r *Renderer, src string) string {
	t.Helper()
	out, err := r.Preprocess([]byte(src), 1, Context{Path: "Town/Town.md"})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func writeFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// renderFull renders a page and derives its HTML, as the build does.
func renderFull(r *Renderer, p string) (*Page, error) {
	pg, err := r.Render(p)
	if err == nil {
		err = r.Finish(pg)
	}
	return pg, err
}
