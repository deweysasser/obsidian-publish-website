package index

import (
	"slices"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/vault"
)

func fixtureIndex(t *testing.T) *Index {
	t.Helper()
	v, err := vault.Load("../../testdata/fixture-vault")
	if err != nil {
		t.Fatal(err)
	}
	return Build(v)
}

func TestResolve(t *testing.T) {
	ix := fixtureIndex(t)
	cases := []struct{ target, from, want string }{
		{"Road Repair", "Daily/2026-10-18.md", "Town/Issues/Road Repair.md"},
		{"road repair", "Daily/2026-10-18.md", "Town/Issues/Road Repair.md"},
		{"Issues/Road Repair", "Town/Town.md", "Town/Issues/Road Repair.md"},
		{"Town/Issues/Road Repair.md", "x.md", "Town/Issues/Road Repair.md"},
		{"../Issues/Road Repair", "Town/Videos/2026-08-19 - Meeting.md", "Town/Issues/Road Repair.md"},
		{"photo.png", "Town/Town.md", "Town/img/photo.png"},
		{"img/photo.png", "Town/Town.md", "Town/img/photo.png"},
		{"Nope", "Town/Town.md", ""},
		{"", "Town/Town.md", ""},
	}
	for _, c := range cases {
		got, ok := ix.Resolve(c.target, c.from)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("Resolve(%q, %q) = %q,%v want %q", c.target, c.from, got, ok, c.want)
		}
	}
}

func TestResolveParensAndUnicode(t *testing.T) {
	ix := fixtureIndex(t)
	for target, want := range map[string]string{
		"Bob (Elm St)":  "Town/People/Bob (Elm St).md",
		"Café Permit":   "Town/Issues/Café Permit.md",
		"Zoning & Lots": "Town/Issues/Zoning & Lots.md",
		"CAFÉ PERMIT":   "Town/Issues/Café Permit.md",
	} {
		if got, _ := ix.Resolve(target, "Town/Town.md"); got != want {
			t.Errorf("%q → %q want %q", target, got, want)
		}
	}
}

func TestResolveAmbiguousPrefersSameFolderThenShortest(t *testing.T) {
	ix := &Index{byName: map[string][]string{}, Pages: map[string]*Page{}}
	for _, p := range []string{"a/x/N.md", "b/N.md", "c/d/e/N.md"} {
		ix.addPath(p)
	}
	if got, _ := ix.Resolve("N", "a/x/other.md"); got != "a/x/N.md" {
		t.Errorf("same folder: %q", got)
	}
	if got, _ := ix.Resolve("N", "z/other.md"); got != "b/N.md" {
		t.Errorf("shortest: %q", got)
	}
}

func TestResolveAlias(t *testing.T) {
	ix := fixtureIndex(t)
	ix.Pages["Town/Issues/Road Repair.md"].Aliases = []string{"Main St Repair"}
	ix.indexAliases()
	if got, _ := ix.Resolve("main st repair", "Town/Town.md"); got != "Town/Issues/Road Repair.md" {
		t.Errorf("alias → %q", got)
	}
}

func TestInlinksAndResolvedLinks(t *testing.T) {
	ix := fixtureIndex(t)
	rr := ix.Pages["Town/Issues/Road Repair.md"]
	for _, want := range []string{"Daily/2026-10-18.md", "Town/Videos/2026-08-19 - Meeting.md", "Town/Issues/Zoning & Lots.md"} {
		if !slices.Contains(rr.Inlinks, want) {
			t.Errorf("inlinks %v missing %s", rr.Inlinks, want)
		}
	}
	if !slices.IsSorted(rr.Inlinks) || slices.Contains(rr.Inlinks, "Town/Issues/Road Repair.md") {
		t.Errorf("inlinks %v", rr.Inlinks)
	}
	if rr.Links[0].Path != "Town/People/Alice Smith.md" {
		t.Errorf("frontmatter link path %q", rr.Links[0].Path)
	}
	if !slices.IsSorted(ix.Paths) || len(ix.Paths) != len(ix.Pages) {
		t.Errorf("paths %v", ix.Paths)
	}
}
