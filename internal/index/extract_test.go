package index

import (
	"reflect"
	"slices"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/vault"
	"gopkg.in/yaml.v3"
)

func extract(t *testing.T, raw string) *Page {
	t.Helper()
	fmb, body, line := vault.SplitFrontmatter([]byte(raw))
	n := &vault.Note{Path: "dir/note.md", Raw: []byte(raw), Body: body, BodyLine: line}
	if fmb != nil {
		n.Frontmatter = map[string]any{}
		if err := yaml.Unmarshal(fmb, &n.Frontmatter); err != nil {
			t.Fatal(err)
		}
	}
	return Extract(n)
}

func linkTargets(ls []Link) []string {
	var out []string
	for _, l := range ls {
		out = append(out, l.Target)
	}
	return out
}

func TestExtractWikilinks(t *testing.T) {
	p := extract(t, "[[A]] [[A|al]] [[B#H]] [[C#^b]] ![[img.png|200]] [[D#H|al2]] [[Bob (Elm St)]]\n[x](E.md) [y](https://e.com) [z](F%20G.md)\n")
	want := []Link{
		{Raw: "A", Target: "A", Line: 1},
		{Raw: "A|al", Target: "A", Display: "al", Line: 1},
		{Raw: "B#H", Target: "B", Subpath: "H", Line: 1},
		{Raw: "C#^b", Target: "C", Subpath: "^b", Line: 1},
		{Raw: "img.png|200", Target: "img.png", Display: "200", Embed: true, Line: 1},
		{Raw: "D#H|al2", Target: "D", Subpath: "H", Display: "al2", Line: 1},
		{Raw: "Bob (Elm St)", Target: "Bob (Elm St)", Line: 1},
		{Raw: "E.md", Target: "E.md", Display: "x", Markdown: true, Line: 2},
		{Raw: "F%20G.md", Target: "F G.md", Display: "z", Markdown: true, Line: 2},
	}
	if !reflect.DeepEqual(p.Links, want) {
		t.Errorf("got\n%#v\nwant\n%#v", p.Links, want)
	}
}

func TestExtractSkipsCode(t *testing.T) {
	p := extract(t, "```\n[[A]] #tag k:: v\n```\n`[[B]]` #ok\n")
	if len(p.Links) != 0 {
		t.Errorf("links %v", p.Links)
	}
	if !reflect.DeepEqual(p.Tags, []string{"#ok"}) {
		t.Errorf("tags %v", p.Tags)
	}
	if len(p.Fields) != 0 {
		t.Errorf("fields %v", p.Fields)
	}
}

func TestExtractTags(t *testing.T) {
	p := extract(t, "---\ntags: [x, \"#y\"]\n---\n# Heading\n#a/b and #123 and x#y and #c-d_e #Café\n")
	want := []string{"#x", "#y", "#a", "#a/b", "#c-d_e", "#Café"}
	if !reflect.DeepEqual(p.Tags, want) {
		t.Errorf("tags %v want %v", p.Tags, want)
	}
	if !reflect.DeepEqual(p.ETags, []string{"#x", "#y", "#a/b", "#c-d_e", "#Café"}) {
		t.Errorf("etags %v", p.ETags)
	}
	p = extract(t, "---\ntags: one, two three\n---\n")
	if !reflect.DeepEqual(p.Tags, []string{"#one", "#two", "#three"}) {
		t.Errorf("string tags %v", p.Tags)
	}
}

func TestExtractInlineFields(t *testing.T) {
	p := extract(t, "priority:: 2\n**Due**:: 2026-01-01\ntext [k:: v] and (h:: hidden) end\n- [ ] task [due:: 2026-10-01]\nnot a field: x\nurl https://x.com::y\n")
	got := map[string]string{}
	for _, f := range p.Fields {
		got[f.Key] = f.Raw
	}
	want := map[string]string{"priority": "2", "Due": "2026-01-01", "k": "v", "h": "hidden", "due": "2026-10-01"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fields %v", got)
	}
	if len(p.Lists) != 1 || len(p.Lists[0].Fields) != 1 || p.Lists[0].Fields[0].Key != "due" {
		t.Errorf("list fields %+v", p.Lists)
	}
}

func TestExtractLists(t *testing.T) {
	p := extract(t, "## Tasks\n\n- [ ] parent #t\n  continuation\n  - [x] child [[L]] ^blk\n- plain\n1. numbered\n")
	if len(p.Lists) != 4 {
		t.Fatalf("lists %+v", p.Lists)
	}
	par, ch, plain := p.Lists[0], p.Lists[1], p.Lists[2]
	if !par.Task || par.Status != " " || par.Text != "parent #t" || par.Parent != -1 || par.LineCount != 2 || !slices.Equal(par.Children, []int{1}) || par.Section != "Tasks" {
		t.Errorf("parent %+v", par)
	}
	if !ch.Task || ch.Status != "x" || ch.Parent != 0 || ch.BlockID != "blk" || len(ch.Links) != 1 || ch.Line != 5 {
		t.Errorf("child %+v", ch)
	}
	if plain.Task || plain.Parent != -1 {
		t.Errorf("plain %+v", plain)
	}
	if !slices.Equal(par.Tags, []string{"#t"}) {
		t.Errorf("tags %v", par.Tags)
	}
}

func TestExtractFrontmatterLinksAndAliases(t *testing.T) {
	p := extract(t, "---\nInvolved:\n  - \"[[Alice Smith]]\"\n  - \"[[Bob (Elm St)|Bob]]\"\naliases: Foo\nsee: \"[[X]]\"\n---\nbody\n")
	if got := linkTargets(p.Links); !slices.Equal(got, []string{"Alice Smith", "Bob (Elm St)", "X"}) {
		t.Errorf("links %v", got)
	}
	if !slices.Equal(p.Aliases, []string{"Foo"}) {
		t.Errorf("aliases %v", p.Aliases)
	}
}

func TestExtractHeadingsAndBlocks(t *testing.T) {
	p := extract(t, "# Title\ntext ^b1\n## Sub **bold**\n```\n# not\n```\n")
	if len(p.Headings) != 2 || p.Headings[1].Text != "Sub **bold**" || p.Headings[1].Level != 2 || p.Headings[1].Line != 3 {
		t.Errorf("headings %+v", p.Headings)
	}
	if p.Blocks["b1"] != 2 {
		t.Errorf("blocks %v", p.Blocks)
	}
}

func TestExtractIgnoresComments(t *testing.T) {
	p := extract(t, "%%\n## Hidden\n- [ ] secret task [[X]] #t k:: v ^b\n%%\nok\n")
	if len(p.Headings)+len(p.Lists)+len(p.Links)+len(p.Tags)+len(p.Fields)+len(p.Blocks) != 0 {
		t.Errorf("commented metadata extracted: %+v", p)
	}
}
