package search

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStableOrder(t *testing.T) {
	for name, c := range map[string]struct {
		prev, current, want []string
		st                  orderStats
	}{
		"first build is path order": {
			nil, []string{"b.html", "a.html", "c/index.html"},
			[]string{"a.html", "b.html", "c/index.html"}, orderStats{New: 3},
		},
		"unchanged keeps the previous order, even when it isn't path order": {
			[]string{"z.html", "a.html"}, []string{"a.html", "z.html"},
			[]string{"z.html", "a.html"}, orderStats{},
		},
		"new pages go at the end, in path order": {
			[]string{"m.html", "b.html"}, []string{"b.html", "m.html", "y.html", "a.html"},
			[]string{"m.html", "b.html", "a.html", "y.html"}, orderStats{New: 2},
		},
		"new pages fill the gaps removed pages leave": {
			[]string{"a.html", "gone1.html", "c.html", "gone2.html", "e.html"}, []string{"a.html", "c.html", "e.html", "n1.html", "n2.html", "n3.html"},
			[]string{"a.html", "n1.html", "c.html", "n2.html", "e.html", "n3.html"}, orderStats{New: 3, Removed: 2},
		},
		"unfilled gaps close up": {
			[]string{"a.html", "gone.html", "c.html", "gone2.html"}, []string{"c.html", "a.html", "n.html"},
			[]string{"a.html", "n.html", "c.html"}, orderStats{New: 1, Removed: 2},
		},
		"a damaged state with duplicates": {
			[]string{"a.html", "a.html", "b.html"}, []string{"a.html", "b.html"},
			[]string{"a.html", "b.html"}, orderStats{},
		},
		"everything removed": {
			[]string{"a.html"}, nil, []string{}, orderStats{Removed: 1},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, st := stableOrder(c.prev, c.current)
			if !reflect.DeepEqual(got, c.want) || st != c.st {
				t.Errorf("got %v %+v, want %v %+v", got, st, c.want, c.st)
			}
		})
	}
}

// Every page keeps its position when one is added at the end: Pagefind's
// numbers for existing pages don't move.
func TestStableOrderKeepsPositions(t *testing.T) {
	prev, _ := stableOrder(nil, []string{"d.html", "b.html", "f.html"})
	next, _ := stableOrder(prev, []string{"a.html", "b.html", "c.html", "d.html", "f.html"})
	for i, p := range prev {
		if next[i] != p {
			t.Errorf("position %d: %s, was %s", i, next[i], p)
		}
	}
}

func TestOrderStateRoundTrip(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sub", "site.json")
	if got := loadOrder(file); got != nil {
		t.Errorf("missing file: %v", got)
	}
	want := []string{"b.html", "a.html"}
	if err := saveOrder(file, want); err != nil {
		t.Fatal(err)
	}
	if got := loadOrder(file); !reflect.DeepEqual(got, want) {
		t.Errorf("loaded %v, want %v", got, want)
	}
	entries, _ := os.ReadDir(filepath.Dir(file))
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
	for _, bad := range []string{"not json", `{"version":2,"pages":["x.html"]}`} {
		os.WriteFile(file, []byte(bad), 0o600)
		if got := loadOrder(file); got != nil {
			t.Errorf("%q: loaded %v, want nothing", bad, got)
		}
	}
	if loadOrder("") != nil || saveOrder("", want) == nil {
		t.Error("no state file: load must be empty and save must fail")
	}
}
