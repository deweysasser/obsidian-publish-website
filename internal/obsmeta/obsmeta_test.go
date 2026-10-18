package obsmeta

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/vault"
)

func build(t *testing.T, raw string) *CachedMetadata {
	t.Helper()
	fm, body, line := vault.SplitFrontmatter([]byte(raw))
	_ = fm
	return Build(&vault.Note{Path: "a.md", Raw: []byte(raw), Body: body, BodyLine: line})
}

func pos(l1, c1, o1, l2, c2, o2 int) Pos {
	return Pos{Start: Loc{l1, c1, o1}, End: Loc{l2, c2, o2}}
}

func TestLinksEmbedsTags(t *testing.T) {
	m := build(t, "x [[A#H|al]] ![[img.png]] #tag\n`[[no]]` %%[[no]]%% [md](B%20C.md) ![](i.png) é[[D]]\n")
	wantLinks := []LinkCache{
		{Link: "A#H", Original: "[[A#H|al]]", DisplayText: "al", Position: pos(0, 2, 2, 0, 12, 12)},
		{Link: "B C.md", Original: "[md](B%20C.md)", DisplayText: "md", Position: pos(1, 20, 51, 1, 34, 65)},
		{Link: "D", Original: "[[D]]", DisplayText: "D", Position: pos(1, 47, 78, 1, 52, 83)},
	}
	if !reflect.DeepEqual(m.Links, wantLinks) {
		t.Errorf("links\n got %+v\nwant %+v", m.Links, wantLinks)
	}
	if len(m.Embeds) != 2 || m.Embeds[0].Link != "img.png" || m.Embeds[0].Original != "![[img.png]]" || m.Embeds[1].Link != "i.png" {
		t.Errorf("embeds %+v", m.Embeds)
	}
	if len(m.Tags) != 1 || m.Tags[0].Tag != "#tag" || m.Tags[0].Position != pos(0, 26, 26, 0, 30, 30) {
		t.Errorf("tags %+v", m.Tags)
	}
}

func TestDisplayTextDefaults(t *testing.T) {
	m := build(t, "[[A#H]] [[A#^b]] [[#H]]")
	got := []string{m.Links[0].DisplayText, m.Links[1].DisplayText, m.Links[2].DisplayText}
	if !reflect.DeepEqual(got, []string{"A > H", "A > ^b", "H"}) {
		t.Errorf("display %v", got)
	}
}

func TestHeadingsAndSections(t *testing.T) {
	raw := "---\na: 1\n---\n# Title\npara one\npara two ^p1\n\n- a\n  - b\n- [x] c ^blk\n\n```js\nk:: v\n```\n> [!note] T\n> body\n\n> quote\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n---\n%%\ncomment\n%%\n$$\nx\n$$\n"
	m := build(t, raw)
	type sec struct {
		typ        string
		start, end int
	}
	want := []sec{{"yaml", 0, 2}, {"heading", 3, 3}, {"paragraph", 4, 5}, {"list", 7, 9}, {"code", 11, 13},
		{"callout", 14, 15}, {"blockquote", 17, 17}, {"table", 19, 21}, {"thematicBreak", 23, 23}, {"comment", 24, 26}, {"math", 27, 29}}
	var got []sec
	for _, s := range m.Sections {
		got = append(got, sec{s.Type, s.Position.Start.Line, s.Position.End.Line})
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sections\n got %v\nwant %v", got, want)
	}
	if m.Sections[2].ID != "p1" {
		t.Errorf("paragraph id %q", m.Sections[2].ID)
	}
	if len(m.Headings) != 1 || m.Headings[0].Heading != "Title" || m.Headings[0].Level != 1 || m.Headings[0].Position != pos(3, 0, 13, 3, 7, 20) {
		t.Errorf("headings %+v", m.Headings)
	}
}

func TestListItems(t *testing.T) {
	m := build(t, "intro\n\n- a\n  cont\n  - b\n    - c\n- [x] d ^blk\n1. e\n\n> - q\n")
	type li struct {
		start, end, parent int
		task               string
		id                 string
	}
	var got []li
	for _, it := range m.ListItems {
		task := ""
		if it.Task != nil {
			task = *it.Task
		}
		got = append(got, li{it.Position.Start.Line, it.Position.End.Line, it.Parent, task, it.ID})
	}
	want := []li{{2, 3, -2, "", ""}, {4, 4, 2, "", ""}, {5, 5, 4, "", ""}, {6, 6, -2, "x", "blk"}, {7, 7, -7, "", ""}, {9, 9, -9, "", ""}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("list items\n got %v\nwant %v", got, want)
	}
}

func TestFrontmatterScalarsAndLinks(t *testing.T) {
	m := build(t, "---\nb: 2026-09-22\na: 1\nc: true\nd:\ne: [\"[[X|x]]\", 3.5]\nf: \"x\"\n---\nbody")
	j, _ := json.Marshal(m.Frontmatter)
	if string(j) != `{"b":"2026-09-22","a":1,"c":true,"d":null,"e":["[[X|x]]",3.5],"f":"x"}` {
		t.Errorf("frontmatter %s", j)
	}
	if len(m.FrontmatterLinks) != 1 || m.FrontmatterLinks[0].Key != "e.0" || m.FrontmatterLinks[0].Link != "X" || m.FrontmatterLinks[0].DisplayText != "x" {
		t.Errorf("frontmatter links %+v", m.FrontmatterLinks)
	}
}

func TestNoFrontmatterIsNull(t *testing.T) {
	m := build(t, "x")
	j, _ := json.Marshal(m)
	var back map[string]any
	json.Unmarshal(j, &back)
	if _, ok := back["frontmatter"]; ok {
		t.Errorf("frontmatter should be omitted: %s", j)
	}
}
