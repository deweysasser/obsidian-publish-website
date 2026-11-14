package build

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/rs/zerolog"
)

// TestNoLeak builds every target and asserts nothing from excluded notes,
// notes outside the root, or private frontmatter reaches the output.
func TestNoLeak(t *testing.T) {
	forbidden := []string{
		"Town/People", "people/", "alice-smith", "bob-elm-st", // paths and slugs of excluded notes
		"headshot",                                           // excluded attachment
		"508-555-0100", "Clerk", "Alice is the", "Bob lives", // excluded note content and frontmatter
		"public-profile",              // excluded note's tag
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
			vendored := walkOutput(t, out, func(rel, s string) {
				for _, f := range forbidden {
					if strings.Contains(s, f) {
						t.Errorf("%s contains %q", rel, f)
					}
				}
				for _, m := range linked.FindAllString(s, -1) {
					if strings.Contains(m, "Alice") || strings.Contains(m, "Bob") {
						t.Errorf("%s links an excluded person: %s", rel, m)
					}
				}
			})
			checkVendorSkipped(t, tgt, vendored)
		})
	}
}

// TestNoLeakWithInclude re-admits the one excluded note carrying the marker
// tag and asserts it is published while everything TestNoLeak protects
// about the rest stays out: the other excluded note, the excluded attachment
// the included note embeds, the hidden marker tag, the included note's
// frontmatter, and the note outside the root.
func TestNoLeakWithInclude(t *testing.T) {
	forbidden := []string{
		"Town/People", "people/bob", "bob-elm-st", // path and slug of the note still excluded
		"headshot",                  // excluded attachment, embedded by the included note
		"508-555-0100", "Bob lives", // content of the note still excluded
		"Clerk", "role:", // included note's frontmatter (not exported by default)
		"public-profile",              // the marker tag, hidden by hidden_tags
		"Daily note", "2026-10-18.md", // note outside root
		"508-555-0177", "private_phone", // published note's frontmatter
	}
	linked := regexp.MustCompile(`<a[^>]*>(Alice Smith|Bob \(Elm St\)|Bob)</a>|\]\([^)]*\)`)
	aliceLink := regexp.MustCompile(`<a [^>]*href="[^"]*people/alice-smith/?"[^>]*>Alice Smith</a>|\[Alice Smith\]\([^)]*people/alice-smith[^)]*\)`)
	for _, tgt := range []string{"html", "hugo", "jekyll"} {
		t.Run(tgt, func(t *testing.T) {
			out := t.TempDir()
			cfg := fixtureConfig(out, tgt)
			cfg.Include = []config.Include{{Path: "People/**", Tags: []string{"#Public-Profile"}}}
			cfg.Site.HiddenTags = []string{"public-profile"}
			rep, err := Run(cfg, zerolog.Nop())
			if err != nil {
				t.Fatal(err)
			}
			if rep.Pages != 7 {
				t.Errorf("pages %d, want the 6 of TestBuildHTMLFixture plus the included note", rep.Pages)
			}
			var aliceFile, aliceContent, aliceLinked bool
			vendored := walkOutput(t, out, func(rel, s string) {
				aliceFile = aliceFile || strings.Contains(rel, "alice-smith")
				aliceContent = aliceContent || strings.Contains(s, "Alice is the clerk")
				aliceLinked = aliceLinked || aliceLink.MatchString(s)
				for _, f := range forbidden {
					if strings.Contains(s, f) {
						t.Errorf("%s contains %q", rel, f)
					}
				}
				for _, m := range linked.FindAllString(s, -1) {
					if strings.Contains(m, "Bob") || strings.Contains(m, "bob-elm-st") {
						t.Errorf("%s links an excluded person: %s", rel, m)
					}
				}
			})
			checkVendorSkipped(t, tgt, vendored)
			if !aliceFile || !aliceContent || !aliceLinked {
				t.Errorf("included note: file %v content %v linked %v", aliceFile, aliceContent, aliceLinked)
			}
		})
	}
}

// checkVendorSkipped fails if the html target's vendored assets weren't
// recognised and skipped: then they would be scanned, or the skip would
// match nothing on this OS.
func checkVendorSkipped(t *testing.T, tgt string, vendored int) {
	t.Helper()
	if tgt == "html" && vendored == 0 {
		t.Error("no vendored assets skipped in the html output")
	}
}

// walkOutput calls check with each output file's path (relative to out,
// with forward slashes on every OS) and contents, skipping vendored assets,
// and returns how many vendored files it skipped. File paths are checked as
// contents too, so a forbidden slug in a file name is caught.
func walkOutput(t *testing.T, out string, check func(rel, s string)) (vendored int) {
	t.Helper()
	n := 0
	err := filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		r, err := filepath.Rel(out, p)
		if err != nil {
			return err
		}
		rel := "/" + filepath.ToSlash(r)
		if strings.Contains(rel, "/assets/vendor/") {
			vendored++
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		n++
		check(rel, rel+"\n"+string(b))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 5 {
		t.Fatalf("only %d files written", n)
	}
	return vendored
}
