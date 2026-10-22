package search

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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
	if err := RunPagefind(context.Background(), r, site, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"pagefind/pagefind.js", "pagefind/pagefind-ui.js", "pagefind/pagefind-entry.json"} {
		if _, err := os.Stat(filepath.Join(site, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
}

type stubRunner struct{}

func (stubRunner) Check(string) error                              { return nil }
func (stubRunner) Run(context.Context, tools.Spec) (string, error) { return "", nil }

func TestRunPagefindLogsElapsed(t *testing.T) {
	var buf bytes.Buffer
	if err := RunPagefind(context.Background(), stubRunner{}, t.TempDir(), zerolog.New(&buf)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"elapsed":`) || !strings.Contains(buf.String(), "Indexed site search") {
		t.Errorf("log: %s", buf.String())
	}
}
