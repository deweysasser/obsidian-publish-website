package dataview

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// plain projects a bridge value onto the oracle's serialization.
func plain(v Value) any {
	switch v.Kind {
	case KNull:
		return nil
	case KBool:
		return v.Bool
	case KNumber:
		return v.Num
	case KString, KText:
		return v.Str
	case KDate:
		return map[string]any{"$date": v.ISO}
	case KDuration:
		return map[string]any{"$dur": v.Num}
	case KLink:
		var sub any
		if v.Link.Subpath != "" {
			sub = v.Link.Subpath
		}
		return map[string]any{"$link": v.Link.Path, "subpath": sub, "type": v.Link.Type}
	case KList:
		out := []any{}
		for _, e := range v.List {
			out = append(out, plain(e))
		}
		return out
	case KObject:
		out := []any{}
		for _, k := range v.Keys {
			out = append(out, []any{k, plain(v.Obj[k])})
		}
		return map[string]any{"$obj": out}
	case KPair:
		return []any{plain(*v.Pair[0]), plain(*v.Pair[1])}
	}
	return v.Str
}

func TestOracleResults(t *testing.T) {
	b, err := os.ReadFile("../../testdata/oracle/results.json")
	if err != nil {
		t.Skip("no oracle goldens; see tools/dv-oracle")
	}
	var want map[string]struct {
		Type    string   `json:"type"`
		Headers []string `json:"headers"`
		Values  any      `json:"values"`
		Error   bool     `json:"error"`
	}
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Name, Origin, Query string }
	cb, _ := os.ReadFile("../../testdata/oracle/cases.json")
	if err := json.Unmarshal(cb, &cases); err != nil {
		t.Fatal(err)
	}
	e := needNode(t)
	e.ix = fixture(t)
	var reqs []Request
	for _, c := range cases {
		reqs = append(reqs, Request{ID: c.Name, Kind: "query", Origin: c.Origin, Source: c.Query})
	}
	got, err := e.RunIndex(reqs)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		w, g := want[c.Name], got[c.Name]
		if w.Error != !g.OK {
			t.Errorf("%s: error mismatch (oracle error=%v, bridge ok=%v %s)", c.Name, w.Error, g.OK, g.Error)
			continue
		}
		if g.Type != "table" {
			continue // list/task shapes differ between dv.query and the bridge; tables carry the values
		}
		var rows []any
		for _, r := range g.Rows {
			var row []any
			for _, v := range r {
				row = append(row, plain(v))
			}
			rows = append(rows, row)
		}
		gj, _ := json.Marshal(rows)
		var gotRows any
		json.Unmarshal(gj, &gotRows)
		if !reflect.DeepEqual(gotRows, w.Values) || !reflect.DeepEqual(g.Headers, w.Headers) {
			wj, _ := json.Marshal(w.Values)
			t.Errorf("%s:\n got %v %s\nwant %v %s", c.Name, g.Headers, gj, w.Headers, wj)
		}
	}
}
