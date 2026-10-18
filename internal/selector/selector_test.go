package selector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/deweysasser/obsidian-publish-website/internal/index"
	"github.com/deweysasser/obsidian-publish-website/internal/vault"
)

func TestSlugifyEdgeCases(t *testing.T) {
	cases := map[string]string{
		"7 Sample Road Drainage Violation": "7-sample-road-drainage-violation",
		"Pat (12B Example Lane)":           "pat-12b-example-lane",
		"Zoning & Lots":                    "zoning-lots",
		"Café Permit":                      "café-permit",
		"Café Permit":                     "café-permit", // NFD input normalizes to NFC
		"2026-08-19 - Meeting":             "2026-08-19-meeting",
		"O'Brien":                          "o-brien",
		"!!!":                              "untitled",
		"":                                 "untitled",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q want %q", in, got, want)
		}
	}
}

func load(t *testing.T, dir string) *index.Index {
	t.Helper()
	v, err := vault.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return index.Build(v)
}

func TestSelectFixture(t *testing.T) {
	ix := load(t, "../../testdata/fixture-vault")
	pub, err := Select(ix, &config.Config{Root: "Town", Exclude: []string{"People/**"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := pub.Pages["Town/Issues/Road Repair.md"]; got != "issues/road-repair" {
		t.Errorf("road repair slug %q", got)
	}
	if got, ok := pub.Pages["Town/Town.md"]; !ok || got != "" {
		t.Errorf("home slug %q %v", got, ok)
	}
	for p := range pub.Pages {
		if strings.HasPrefix(p, "Town/People/") || !strings.HasPrefix(p, "Town/") {
			t.Errorf("published %s", p)
		}
	}
	if !pub.IsPublished("Town/Current Issues.md") || pub.IsPublished("Town/People/Alice Smith.md") || pub.IsPublished("Daily/2026-10-18.md") {
		t.Error("IsPublished")
	}
	if !pub.IsAssetAllowed("Town/img/photo.png") || pub.IsAssetAllowed("Town/People/headshot.png") || pub.IsAssetAllowed("elsewhere.png") {
		t.Error("IsAssetAllowed")
	}
	if got := pub.MarkAsset("Town/img/photo.png"); got != "img/photo.png" || pub.Assets["Town/img/photo.png"] != "img/photo.png" {
		t.Errorf("MarkAsset %q", got)
	}
	if pub.Folders["Town"] != "" || pub.Folders["Town/Issues"] != "issues" {
		t.Errorf("folders %v", pub.Folders)
	}
	if _, ok := pub.Folders["Town/People"]; ok {
		t.Error("excluded folder listed")
	}
	if pub.FolderNote("Town") != "Town/Town.md" || pub.FolderNote("Town/Issues") != "" {
		t.Error("FolderNote")
	}
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPublishFalseAndHomeAndFolderNotes(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "R/a.md", "a")
	write(t, dir, "R/hidden.md", "---\npublish: false\n---\nx")
	write(t, dir, "R/hidden2.md", "---\npublish: \"false\"\n---\nx")
	write(t, dir, "R/Sub/Sub.md", "folder note")
	write(t, dir, "R/Other/index.md", "index note")
	write(t, dir, "R/Other/x.md", "x")
	write(t, dir, "R/Drafts/d.md", "d")
	pub, err := Select(load(t, dir), &config.Config{Root: "R", Exclude: []string{"Drafts"}, Site: config.Site{Home: "a.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if pub.IsPublished("R/hidden.md") || pub.IsPublished("R/hidden2.md") || pub.IsPublished("R/Drafts/d.md") {
		t.Error("excluded note published")
	}
	if pub.Pages["R/a.md"] != "" || pub.Pages["R/Sub/Sub.md"] != "sub" || pub.Pages["R/Other/index.md"] != "other" || pub.Pages["R/Other/x.md"] != "other/x" {
		t.Errorf("slugs %v", pub.Pages)
	}
}

func TestSlugCollision(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "R/A b.md", "1")
	write(t, dir, "R/a-b.md", "2")
	_, err := Select(load(t, dir), &config.Config{Root: "R"})
	if err == nil || !strings.Contains(err.Error(), "R/A b.md") || !strings.Contains(err.Error(), "R/a-b.md") {
		t.Fatalf("want collision error, got %v", err)
	}
}

func TestRootMissing(t *testing.T) {
	_, err := Select(load(t, "../../testdata/fixture-vault"), &config.Config{Root: "Nope"})
	if err == nil {
		t.Fatal("want error for empty root")
	}
}
