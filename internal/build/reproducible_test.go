package build

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/rs/zerolog"
)

// TestRebuildIsByteIdentical builds the fixture vault twice and requires
// the same files with the same bytes. A deploy that publishes only what
// changed (an R2 sync, a git commit) relies on this: otherwise an unchanged
// vault re-publishes files on every run. Output that changes by date by
// design (security.txt's Expires, Dataview's date(today)) is not configured
// here.
func TestRebuildIsByteIdentical(t *testing.T) {
	requireNode(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	a, b := t.TempDir(), t.TempDir()
	for _, out := range []string{a, b} {
		cfg := fixtureConfig(out, "html")
		cfg.Site.Feed = &config.Feed{From: "Issues"}
		cfg.Site.Redirects = map[string]string{"/old-town/": "Town.md", "/old-issues/": "Current Issues.md"}
		cfg.ApplyDefaults()
		if err := cfg.Validate(); err != nil {
			t.Fatal(err)
		}
		if _, err := Run(cfg, zerolog.Nop()); err != nil {
			t.Fatal(err)
		}
	}
	files := func(dir string) map[string][]byte {
		m := map[string][]byte{}
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(dir, p)
			m[rel], err = os.ReadFile(p) // #nosec G304 -- a test's own output
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	fa, fb := files(a), files(b)
	if len(fa) < 10 {
		t.Fatalf("only %d files built", len(fa))
	}
	for _, want := range []string{"feed.xml", filepath.Join("old-town", "index.html")} {
		if _, ok := fa[want]; !ok {
			t.Errorf("%s not built; the test should cover it", want)
		}
	}
	for rel, ba := range fa {
		bb, ok := fb[rel]
		switch {
		case !ok:
			t.Errorf("%s: only in the first build", rel)
		case !bytes.Equal(ba, bb):
			t.Errorf("%s: differs between builds", rel)
		}
	}
	for rel := range fb {
		if _, ok := fa[rel]; !ok {
			t.Errorf("%s: only in the second build", rel)
		}
	}
}
