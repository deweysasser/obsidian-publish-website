package build

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// TestNoLeak builds every target and asserts nothing from excluded notes,
// notes outside the root, or private frontmatter reaches the output.
func TestNoLeak(t *testing.T) {
	forbidden := []string{
		"Town/People", "people/", "alice-smith", "bob-elm-st", // paths and slugs of excluded notes
		"headshot",                                           // excluded attachment
		"508-555-0100", "Clerk", "Alice is the", "Bob lives", // excluded note content and frontmatter
		"Daily note", "2026-10-18.md", // note outside root
		"508-555-0177", "private_phone", // published note's frontmatter (not exported by default)
	}
	linked := regexp.MustCompile(`<a[^>]*>(Alice Smith|Bob \(Elm St\)|Bob)</a>|\]\([^)]*\)`)
	for _, tgt := range []string{"html", "hugo", "jekyll"} {
		t.Run(tgt, func(t *testing.T) {
			out := t.TempDir()
			if _, err := Run(fixtureConfig(out, tgt), zerolog.Nop()); err != nil {
				t.Fatal(err)
			}
			n := 0
			err := filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() || strings.Contains(p, "/assets/vendor/") {
					return err
				}
				b, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				n++
				s := string(b)
				for _, f := range forbidden {
					if strings.Contains(s, f) {
						t.Errorf("%s contains %q", strings.TrimPrefix(p, out), f)
					}
				}
				for _, m := range linked.FindAllString(s, -1) {
					if strings.Contains(m, "Alice") || strings.Contains(m, "Bob") {
						t.Errorf("%s links an excluded person: %s", strings.TrimPrefix(p, out), m)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if n < 5 {
				t.Fatalf("only %d files written", n)
			}
		})
	}
}
