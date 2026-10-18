package vault

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const fixture = "../../testdata/fixture-vault"

func TestSplitFrontmatter(t *testing.T) {
	fm, body, line := SplitFrontmatter([]byte("---\na: 1\n---\nhello\n"))
	if string(fm) != "a: 1\n" || string(body) != "hello\n" || line != 4 {
		t.Errorf("got fm=%q body=%q line=%d", fm, body, line)
	}
	fm, body, line = SplitFrontmatter([]byte("hello\n"))
	if fm != nil || string(body) != "hello\n" || line != 1 {
		t.Errorf("no fm: got fm=%q body=%q line=%d", fm, body, line)
	}
	raw := "---\na: 1\n"
	fm, body, line = SplitFrontmatter([]byte(raw))
	if fm != nil || string(body) != raw || line != 1 {
		t.Errorf("unterminated: got fm=%q body=%q line=%d", fm, body, line)
	}
	fm, body, _ = SplitFrontmatter([]byte("---\r\na: 1\r\n---\r\nx"))
	if string(fm) != "a: 1\r\n" || string(body) != "x" {
		t.Errorf("crlf: got fm=%q body=%q", fm, body)
	}
	fm, body, line = SplitFrontmatter([]byte("---\n---\nbody"))
	if fm == nil || len(fm) != 0 || string(body) != "body" || line != 3 {
		t.Errorf("empty fm: got fm=%q body=%q line=%d", fm, body, line)
	}
}

func TestLoadFixture(t *testing.T) {
	v, err := Load(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range v.Notes {
		if strings.Contains(n.Path, ".obsidian") {
			t.Errorf("hidden note loaded: %s", n.Path)
		}
	}
	for _, a := range v.Attachments {
		if strings.Contains(a, ".obsidian") {
			t.Errorf("hidden attachment loaded: %s", a)
		}
	}
	n := v.Note("Town/Issues/Road Repair.md")
	if n == nil {
		t.Fatal("Road Repair not loaded")
	}
	if n.Frontmatter["status"] != "open" {
		t.Errorf("status = %v", n.Frontmatter["status"])
	}
	if !strings.HasPrefix(string(n.Body), "\n## Context") || n.BodyLine != 10 {
		t.Errorf("body starts %q line %d", string(n.Body)[:12], n.BodyLine)
	}
	if n.Mtime.IsZero() || n.Ctime.IsZero() || n.Size == 0 {
		t.Errorf("times/size not set: %+v", n)
	}
	if !slices.Contains(v.Attachments, "Town/img/photo.png") {
		t.Errorf("attachments %v", v.Attachments)
	}
	if !slices.IsSortedFunc(v.Notes, func(a, b *Note) int { return strings.Compare(a.Path, b.Path) }) {
		t.Error("notes not sorted")
	}
	if v.Note("Town/Café Permit.md") != nil || v.Note("Town/Issues/Café Permit.md") == nil {
		t.Error("unicode path lookup")
	}
}

func TestInvalidYAMLWarns(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bad.md"), []byte("---\n: : :\n  - [\n---\nbody\n"), 0o644)
	v, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := v.Note("bad.md")
	if n == nil || n.Frontmatter == nil || len(n.Frontmatter) != 0 {
		t.Fatalf("frontmatter %+v", n)
	}
	if len(v.Warnings) != 1 || !strings.Contains(v.Warnings[0], "bad.md") {
		t.Errorf("warnings %v", v.Warnings)
	}
}
