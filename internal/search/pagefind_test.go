package search

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/tools"
	"github.com/rs/zerolog"
)

// TestRunPagefind runs the real pagefind through the configured runner when
// OBSIDIAN_PUBLISH_TOOLS_RUNNER is set (docker or host); it is skipped otherwise.
func TestRunPagefind(t *testing.T) {
	mode := os.Getenv("OBSIDIAN_PUBLISH_TOOLS_RUNNER")
	if mode == "" {
		t.Skip("set OBSIDIAN_PUBLISH_TOOLS_RUNNER=docker|host to run pagefind")
	}
	r := tools.New(mode, "")
	if err := r.Check("pagefind"); err != nil {
		t.Skip(err)
	}
	site := t.TempDir()
	os.WriteFile(filepath.Join(site, "index.html"), []byte(`<html><body><main data-pagefind-body><h1>Hello</h1><p>Main road</p></main></body></html>`), 0o644)
	state := filepath.Join(t.TempDir(), "order.json")
	if err := RunPagefind(context.Background(), r, site, state, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"pagefind/pagefind.js", "pagefind/pagefind-ui.js", "pagefind/pagefind-entry.json"} {
		if _, err := os.Stat(filepath.Join(site, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	for _, f := range []string{indexerFile, orderFile} {
		if _, err := os.Stat(filepath.Join(site, f)); err == nil {
			t.Errorf("%s left in the site", f)
		}
	}
	if got := loadOrder(state); len(got) != 1 || got[0] != "index.html" {
		t.Errorf("saved order %v", got)
	}
}

// TestRunPagefindStableChunks checks the point of the stable order with the
// real Pagefind: adding a small page renames only the index chunks that hold
// its words. The control, indexed without saved state (path order, where
// the new page comes first), renames nearly every chunk.
func TestRunPagefindStableChunks(t *testing.T) {
	mode := os.Getenv("OBSIDIAN_PUBLISH_TOOLS_RUNNER")
	if mode == "" {
		t.Skip("set OBSIDIAN_PUBLISH_TOOLS_RUNNER=docker|host to run pagefind")
	}
	r := tools.New(mode, "")
	page := func(i int) string {
		var b strings.Builder
		for j := range 2000 { // ~120 pages × 2000 words: about 20 index chunks
			fmt.Fprintf(&b, "w%dx%d ", (i*7+j)%3000, j%5)
		}
		return `<html><body><main data-pagefind-body><p>` + b.String() + `</p></main></body></html>`
	}
	chunks := func(site string) map[string]bool {
		m := map[string]bool{}
		entries, _ := os.ReadDir(filepath.Join(site, "pagefind", "index"))
		for _, e := range entries {
			m[e.Name()] = true
		}
		return m
	}
	build := func(state string, added bool) map[string]bool {
		site := t.TempDir()
		for i := range 120 {
			os.WriteFile(filepath.Join(site, fmt.Sprintf("p%03d.html", i)), []byte(page(i)), 0o644)
		}
		if added { // sorts before every other page in path order
			os.WriteFile(filepath.Join(site, "a-new.html"), []byte(`<html><body><main data-pagefind-body><p>zebra quokka</p></main></body></html>`), 0o644)
		}
		if err := RunPagefind(context.Background(), r, site, state, zerolog.Nop()); err != nil {
			t.Fatal(err)
		}
		return chunks(site)
	}
	renamed := func(before, after map[string]bool) int {
		n := 0
		for c := range before {
			if !after[c] {
				n++
			}
		}
		return n
	}
	state := filepath.Join(t.TempDir(), "order.json")
	before := build(state, false)
	stable := renamed(before, build(state, true))
	unordered := renamed(before, build("", true))
	t.Logf("%d index chunks; adding a page renamed %d in stable order, %d in path order", len(before), stable, unordered)
	if len(before) < 10 {
		t.Fatalf("only %d chunks: the test site is too small to tell", len(before))
	}
	if stable > 2 || unordered < len(before)/2 {
		t.Errorf("stable order renamed %d, path order %d, of %d chunks", stable, unordered, len(before))
	}
}

type stubRunner struct{}

func (stubRunner) Check(string) error                              { return nil }
func (stubRunner) Run(context.Context, tools.Spec) (string, error) { return "", nil }

func TestRunPagefindLogsElapsed(t *testing.T) {
	var buf bytes.Buffer
	if err := RunPagefind(context.Background(), stubRunner{}, t.TempDir(), "", zerolog.New(&buf)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"elapsed":"`) || !strings.Contains(buf.String(), `"elapsed_ms":`) || !strings.Contains(buf.String(), "Indexed site search") {
		t.Errorf("log: %s", buf.String())
	}
}

// recorder is a runner that records each call, reading the order file the
// indexer would read, and answers from a script.
type recorder struct {
	t       *testing.T
	noNode  bool
	answers []error // per call; nil past the end
	outputs []string
	calls   []tools.Spec
	orders  [][]string
}

func (r *recorder) Check(tool string) error {
	if tool == "node" && r.noNode {
		return errors.New("node not found")
	}
	return nil
}

func (r *recorder) Run(_ context.Context, s tools.Spec) (string, error) {
	n := len(r.calls)
	r.calls = append(r.calls, s)
	if s.Tool == "node" {
		for _, f := range []string{indexerFile, orderFile} {
			if _, err := os.Stat(filepath.Join(s.Site, f)); err != nil {
				r.t.Errorf("%s not in the site during the run", f)
			}
		}
		b, _ := os.ReadFile(filepath.Join(s.Site, orderFile))
		r.orders = append(r.orders, strings.Fields(string(b)))
	}
	var out string
	var err error
	if n < len(r.outputs) {
		out = r.outputs[n]
	}
	if n < len(r.answers) {
		err = r.answers[n]
	}
	return out, err
}

func siteWith(t *testing.T, pages ...string) string {
	t.Helper()
	site := t.TempDir()
	for _, p := range pages {
		os.MkdirAll(filepath.Dir(filepath.Join(site, p)), 0o755)
		os.WriteFile(filepath.Join(site, p), []byte("<html></html>"), 0o644)
	}
	os.WriteFile(filepath.Join(site, "style.css"), nil, 0o644)
	os.MkdirAll(filepath.Join(site, "pagefind"), 0o755)
	os.WriteFile(filepath.Join(site, "pagefind", "old.html"), nil, 0o644)
	return site
}

func TestRunPagefindKeepsOrderAcrossBuilds(t *testing.T) {
	state := filepath.Join(t.TempDir(), "order.json")
	r := &recorder{t: t}
	if err := RunPagefind(context.Background(), r, siteWith(t, "m/index.html", "b.html"), state, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := RunPagefind(context.Background(), r, siteWith(t, "a.html", "b.html", "m/index.html"), state, zerolog.New(&buf)); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"b.html", "m/index.html"}, {"b.html", "m/index.html", "a.html"}}
	if !reflect.DeepEqual(r.orders, want) {
		t.Errorf("orders %v, want %v", r.orders, want)
	}
	c := r.calls[1]
	if c.Tool != "node" || !reflect.DeepEqual(c.Args, []string{"{site}/" + indexerFile, "{site}", "{site}/" + orderFile}) || c.SiteReadOnly {
		t.Errorf("call %+v", c)
	}
	for _, f := range []string{indexerFile, orderFile} {
		if _, err := os.Stat(filepath.Join(c.Site, f)); err == nil {
			t.Errorf("%s left in the site", f)
		}
	}
	for _, s := range []string{`"pages":3`, `"stable_order":true`, `"new":1`, `"removed":0`} {
		if !strings.Contains(buf.String(), s) {
			t.Errorf("log lacks %s: %s", s, buf.String())
		}
	}
}

func TestRunPagefindFallsBackToCLI(t *testing.T) {
	for name, r := range map[string]*recorder{
		"no Node API": {answers: []error{errors.New("exit status 3")}, outputs: []string{apiUnavailable + ": Cannot find module"}},
		"no node":     {noNode: true},
	} {
		t.Run(name, func(t *testing.T) {
			r.t = t
			state := filepath.Join(t.TempDir(), "order.json")
			var buf bytes.Buffer
			if err := RunPagefind(context.Background(), r, siteWith(t, "a.html"), state, zerolog.New(&buf)); err != nil {
				t.Fatal(err)
			}
			last := r.calls[len(r.calls)-1]
			if last.Tool != "pagefind" || !reflect.DeepEqual(last.Args, []string{"--site", "{site}"}) {
				t.Errorf("last call %+v, want the pagefind CLI", last)
			}
			if !strings.Contains(buf.String(), `"stable_order":false`) || !strings.Contains(buf.String(), `"level":"warn"`) {
				t.Errorf("log: %s", buf.String())
			}
			if _, err := os.Stat(state); err == nil {
				t.Error("order saved although the CLI numbered the pages")
			}
		})
	}
}

func TestRunPagefindErrorIsNotAFallback(t *testing.T) {
	r := &recorder{t: t, answers: []error{errors.New("exit status 1")}, outputs: []string{"Error: bad page"}}
	state := filepath.Join(t.TempDir(), "order.json")
	if err := RunPagefind(context.Background(), r, siteWith(t, "a.html"), state, zerolog.Nop()); err == nil {
		t.Fatal("want the indexer's error")
	}
	if len(r.calls) != 1 {
		t.Errorf("%d calls: a real failure must not retry with the CLI", len(r.calls))
	}
	if _, err := os.Stat(state); err == nil {
		t.Error("order saved after a failed index")
	}
}
