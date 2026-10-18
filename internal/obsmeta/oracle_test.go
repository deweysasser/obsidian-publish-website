package obsmeta

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/vault"
)

// projection keeps the fields Dataview reads, so the comparison ignores
// metadata we intentionally do not emulate.
type projection struct {
	Links     [][3]any `json:"links"`
	Embeds    [][3]any `json:"embeds"`
	Tags      [][2]any `json:"tags"`
	Headings  [][3]any `json:"headings"`
	Sections  [][3]any `json:"sections"`
	ListItems [][5]any `json:"listItems"`
	FMLinks   []string `json:"frontmatterLinks"`
}

func project(m *CachedMetadata) projection {
	var p projection
	for _, l := range m.Links {
		p.Links = append(p.Links, [3]any{l.Link, l.DisplayText, l.Position.Start.Line})
	}
	for _, l := range m.Embeds {
		p.Embeds = append(p.Embeds, [3]any{l.Link, l.DisplayText, l.Position.Start.Line})
	}
	for _, t := range m.Tags {
		p.Tags = append(p.Tags, [2]any{t.Tag, t.Position.Start.Line})
	}
	for _, h := range m.Headings {
		p.Headings = append(p.Headings, [3]any{h.Heading, h.Level, h.Position.Start.Line})
	}
	for _, s := range m.Sections {
		p.Sections = append(p.Sections, [3]any{s.Type, s.Position.Start.Line, s.Position.End.Line})
	}
	for _, it := range m.ListItems {
		task := ""
		if it.Task != nil {
			task = *it.Task
		}
		p.ListItems = append(p.ListItems, [5]any{it.Position.Start.Line, it.Position.End.Line, it.Parent, task, it.ID})
	}
	for _, f := range m.FrontmatterLinks {
		p.FMLinks = append(p.FMLinks, f.Link)
	}
	return p
}

func normalize(t *testing.T, v any) any {
	b, _ := json.Marshal(v)
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOracleMetadata(t *testing.T) {
	b, err := os.ReadFile("../../testdata/oracle/metadata.json")
	if err != nil {
		t.Skip("no oracle goldens; see tools/dv-oracle")
	}
	var want map[string]*CachedMetadata
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Load("../../testdata/fixture-vault")
	if err != nil {
		t.Fatal(err)
	}
	for path, w := range want {
		n := v.Note(path)
		if n == nil {
			t.Errorf("%s: missing from vault", path)
			continue
		}
		got, exp := normalize(t, project(Build(n))), normalize(t, project(w))
		if !reflect.DeepEqual(got, exp) {
			g, _ := json.MarshalIndent(got, "", " ")
			e, _ := json.MarshalIndent(exp, "", " ")
			t.Errorf("%s:\n got %s\nwant %s", path, g, e)
		}
	}
}
