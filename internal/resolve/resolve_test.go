package resolve

import (
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/deweysasser/obsidian-publish-website/internal/index"
	"github.com/deweysasser/obsidian-publish-website/internal/selector"
	"github.com/deweysasser/obsidian-publish-website/internal/urls"
	"github.com/deweysasser/obsidian-publish-website/internal/vault"
)

func fixture(t *testing.T) *Resolver {
	t.Helper()
	v, err := vault.Load("../../testdata/fixture-vault")
	if err != nil {
		t.Fatal(err)
	}
	ix := index.Build(v)
	pub, err := selector.Select(ix, &config.Config{Root: "Town", Exclude: []string{"People/**"}})
	if err != nil {
		t.Fatal(err)
	}
	return &Resolver{Ix: ix, Pub: pub, URLs: urls.HTML("/c/")}
}

func TestHeadingID(t *testing.T) {
	for in, want := range map[string]string{
		"Context": "context", "Log & Notes": "log-notes", "**Bold** title": "bold-title", "Café": "café",
		"A [[Link|alias]]": "a-alias", "under_score": "under_score", "  trim  ": "trim",
	} {
		if got := HeadingID(in); got != want {
			t.Errorf("HeadingID(%q) = %q want %q", in, got, want)
		}
	}
}

func TestLink(t *testing.T) {
	r := fixture(t)
	from := "Town/Town.md"
	cases := []struct {
		link      index.Link
		published bool
		url, text string
	}{
		{index.Link{Target: "Road Repair", Path: "Town/Issues/Road Repair.md"}, true, "/c/issues/road-repair/", "Road Repair"},
		{index.Link{Target: "Road Repair", Display: "the road", Path: "Town/Issues/Road Repair.md"}, true, "/c/issues/road-repair/", "the road"},
		{index.Link{Target: "Road Repair", Subpath: "Context", Path: "Town/Issues/Road Repair.md"}, true, "/c/issues/road-repair/#context", "Road Repair > Context"},
		{index.Link{Target: "2026-08-19 - Meeting", Subpath: "^blk1", Path: "Town/Videos/2026-08-19 - Meeting.md"}, true, "/c/videos/2026-08-19-meeting/#block-blk1", "2026-08-19 - Meeting > ^blk1"},
		{index.Link{Target: "Alice Smith", Path: "Town/People/Alice Smith.md"}, false, "", "Alice Smith"},
		{index.Link{Target: "Road Repair", Path: "Daily/2026-10-18.md"}, false, "", "Road Repair"},
		{index.Link{Target: "Nope"}, false, "", "Nope"},
		{index.Link{Subpath: "Context", Path: "Town/Town.md"}, true, "/c/#context", "Context"},
		{index.Link{Target: "photo.png", Path: "Town/img/photo.png"}, true, "/c/assets/img/photo.png", "photo.png"},
		{index.Link{Target: "headshot.png", Path: "Town/People/headshot.png"}, false, "", "headshot.png"},
	}
	for _, c := range cases {
		got := r.Link(c.link, from)
		if got.Published != c.published || got.URL != c.url || got.Text != c.text {
			t.Errorf("Link(%+v) = %+v", c.link, got)
		}
	}
	if r.Pub.Assets["Town/img/photo.png"] == "" || r.Pub.Assets["Town/People/headshot.png"] != "" {
		t.Errorf("assets %v", r.Pub.Assets)
	}
}
