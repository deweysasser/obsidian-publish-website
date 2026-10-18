package build

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/dataview"
	"github.com/rs/zerolog"
)

func requireNode(t *testing.T) {
	t.Helper()
	if _, err := dataview.FindNode(); err != nil {
		t.Skip(err)
	}
}

// copyFixture copies the fixture vault so tests can add notes.
func copyFixture(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(fixtureVault, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(fixtureVault, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

const probe = "secret:: %% COMMENTSECRET %%\n%%\nmulti:: MULTISECRET\n%%\n\n" +
	"```dataview\nTABLE file.path, role, file.frontmatter FROM \"Town/People\"\n```\n\n" +
	"```dataview\nTASK FROM \"\"\n```\n\n" +
	"```dataview\nTABLE secret, multi FROM \"Town\" WHERE file.name = \"Probe\"\n```\n\n" +
	"```dataview\nLIST FROM [[Road Repair]]\n```\n\n" +
	"```dataview\nTABLE WITHOUT ID length(Involved) AS N, Involved FROM \"Town/Issues\" WHERE Involved\n```\n\n" +
	"Role: `= [[Alice Smith]].role` Phone: `= [[Bob (Elm St)]].file.path`\n"

func TestNoLeakViaDataview(t *testing.T) {
	requireNode(t)
	v := copyFixture(t)
	os.WriteFile(filepath.Join(v, "Town", "Probe.md"), []byte(probe), 0o644)
	os.WriteFile(filepath.Join(v, "Town", "People", "Alice Smith.md"), []byte("---\nrole: Clerk\n---\n\n- [ ] call lawyer PRIVATE\n"), 0o644)
	os.WriteFile(filepath.Join(v, "Daily", "2026-10-18.md"), []byte("- [ ] DAILYTASK [[Road Repair]]\n"), 0o644)
	for _, tgt := range []string{"html", "hugo", "jekyll"} {
		t.Run(tgt, func(t *testing.T) {
			out := t.TempDir()
			cfg := fixtureConfig(out, tgt)
			cfg.Vault = v
			if _, err := Run(cfg, zerolog.Nop()); err != nil {
				t.Fatal(err)
			}
			var all strings.Builder
			filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() && !strings.Contains(filepath.ToSlash(p), "/assets/vendor/") {
					b, _ := os.ReadFile(p)
					all.Write(b)
				}
				return nil
			})
			s := all.String()
			for _, f := range []string{"COMMENTSECRET", "MULTISECRET", "Clerk", "PRIVATE", "DAILYTASK", "Town/People", "Daily/"} {
				if strings.Contains(s, f) {
					t.Errorf("output contains %q", f)
				}
			}
			// links to excluded notes still resolve, so counts over them work
			if !strings.Contains(s, "<td>2</td>") || !strings.Contains(s, "Alice Smith") {
				t.Error("length(Involved) / excluded names should still render as text")
			}
		})
	}
}
