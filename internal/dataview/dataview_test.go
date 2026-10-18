package dataview

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/index"
	"github.com/deweysasser/obsidian-publish-website/internal/vault"
)

func TestDecodeValue(t *testing.T) {
	cases := map[string]Value{
		`null`: {Kind: KNull},
		`true`: {Kind: KBool, Bool: true},
		`2.5`:  {Kind: KNumber, Num: 2.5},
		`"s"`:  {Kind: KString, Str: "s"},
		`{"$date":"2026-09-22T00:00:00.000-04:00","text":"2026-09-22"}`:             {Kind: KDate, Str: "2026-09-22"},
		`{"$dur":86400000,"text":"1 day"}`:                                          {Kind: KDuration, Str: "1 day", Num: 86400000},
		`{"$link":"a.md","subpath":"H","type":"header","display":"x","embed":true}`: {Kind: KLink, Link: Link{Path: "a.md", Subpath: "H", Type: "header", Display: "x", Embed: true}},
		`{"$elink":"https://e.com","display":"e"}`:                                  {Kind: KExternalLink, URL: "https://e.com", Str: "e"},
		`{"$widget":"md"}`: {Kind: KText, Str: "md"},
	}
	for in, want := range cases {
		var got Value
		if err := json.Unmarshal([]byte(in), &got); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got.Kind != want.Kind || got.Str != want.Str || got.Num != want.Num || got.Bool != want.Bool || got.Link != want.Link || got.URL != want.URL {
			t.Errorf("%s → %+v want %+v", in, got, want)
		}
	}
	var l Value
	json.Unmarshal([]byte(`[1,{"$obj":[["b",2],["a",null]]},{"$pair":["k",3]}]`), &l)
	if l.Kind != KList || len(l.List) != 3 || l.List[1].Kind != KObject || strings.Join(l.List[1].Keys, ",") != "b,a" ||
		l.List[1].Obj["b"].Num != 2 || l.List[2].Kind != KPair || l.List[2].Pair[0].Str != "k" || l.List[2].Pair[1].Num != 3 {
		t.Errorf("composite %+v", l)
	}
}

func fixture(t *testing.T) *index.Index {
	t.Helper()
	v, err := vault.Load("../../testdata/fixture-vault")
	if err != nil {
		t.Fatal(err)
	}
	return index.Build(v)
}

func needNode(t *testing.T) *Engine {
	t.Helper()
	node, err := FindNode()
	if err != nil {
		t.Skip(err)
	}
	return &Engine{Node: node, Settings: map[string]any{"defaultDateFormat": "y-MM-dd"}}
}

func TestRunFixtureQueries(t *testing.T) {
	e := needNode(t)
	ix := fixture(t)
	origin := "Town/Current Issues.md"
	res, err := e.Run(FilesFromIndex(ix, nil), []Request{
		{ID: "open", Kind: "query", Origin: origin, Source: `TABLE WITHOUT ID file.link AS Issue, board AS Board, last_updated AS "Last updated", length(Involved) AS People
FROM "Town/Issues" WHERE status = "open" SORT last_updated DESC`},
		{ID: "group", Kind: "query", Origin: origin, Source: `TABLE WITHOUT ID status AS Status, length(rows) AS Count, rows.file.link AS Issues FROM "Town/Issues" WHERE status GROUP BY status SORT status ASC`},
		{ID: "flatten", Kind: "query", Origin: origin, Source: `TABLE person AS Person FROM "Town/Issues" FLATTEN Involved AS person WHERE person`},
		{ID: "list", Kind: "query", Origin: origin, Source: `LIST FROM [[Road Repair]] SORT file.name`},
		{ID: "task", Kind: "query", Origin: origin, Source: `TASK FROM "Town/Videos" WHERE !completed`},
		{ID: "inline", Kind: "inline", Origin: "Town/Issues/Road Repair.md", Source: `this.last_updated + dur(1 week)`},
		{ID: "bad", Kind: "query", Origin: origin, Source: `TABLE nope( FROM`},
		{ID: "cal", Kind: "query", Origin: origin, Source: `CALENDAR file.mtime`},
	})
	if err != nil {
		t.Fatal(err)
	}
	open := res["open"]
	if !open.OK || open.Type != "table" || strings.Join(open.Headers, "|") != "Issue|Board|Last updated|People" || open.IDColumn || len(open.Rows) != 2 {
		t.Fatalf("open %+v", open)
	}
	if open.Rows[0][0].Link.Path != "Town/Issues/Café Permit.md" || open.Rows[0][2].Str != "2026-09-30" || open.Rows[1][3].Num != 2 || open.Rows[0][1].Kind != KNull {
		t.Errorf("open rows %+v", open.Rows)
	}
	g := res["group"]
	if !g.Grouped || len(g.Rows) != 2 || g.Rows[0][0].Str != "open" || len(g.Rows[0][2].List) != 2 {
		t.Errorf("group %+v", g)
	}
	f := res["flatten"]
	if !f.IDColumn || len(f.Rows) != 2 || f.Rows[0][1].Link.Path != "Town/People/Alice Smith.md" {
		t.Errorf("flatten %+v", f)
	}
	if l := res["list"]; l.Type != "list" || len(l.Items) != 3 {
		t.Errorf("list %+v", l)
	}
	if tk := res["task"]; tk.Type != "task" || len(tk.TaskGroups) != 1 || len(tk.TaskGroups[0].Tasks) != 1 || !strings.HasPrefix(tk.TaskGroups[0].Tasks[0].Text, "follow up") || tk.TaskGroups[0].Tasks[0].Path != "Town/Videos/2026-08-19 - Meeting.md" {
		t.Errorf("task %+v", tk)
	}
	if in := res["inline"]; !in.OK || in.Value.Kind != KDate || in.Value.Str != "2026-09-29" {
		t.Errorf("inline %+v", in)
	}
	if b := res["bad"]; b.OK || !strings.Contains(b.Error, "PARSING FAILED") {
		t.Errorf("bad %+v", b)
	}
	if c := res["cal"]; c.OK || !c.Unsupported {
		t.Errorf("calendar %+v", c)
	}
}

func TestFindNodeMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := FindNode(); err == nil || !strings.Contains(err.Error(), "dataview: none") {
		t.Errorf("err %v", err)
	}
}

func TestLoadSettings(t *testing.T) {
	s, err := LoadSettings("../../testdata/fixture-vault")
	if err != nil || s["defaultDateFormat"] != "y-MM-dd" {
		t.Errorf("settings %v err %v", s, err)
	}
	s, err = LoadSettings(t.TempDir())
	if err != nil || len(s) != 0 {
		t.Errorf("missing settings %v %v", s, err)
	}
}
