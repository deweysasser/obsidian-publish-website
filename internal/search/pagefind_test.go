package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestFindPagefindMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := FindPagefind(); err == nil || !strings.Contains(err.Error(), "search: none") {
		t.Errorf("err %v", err)
	}
}

func TestRunPagefind(t *testing.T) {
	bin, err := FindPagefind()
	if err != nil {
		t.Skip(err)
	}
	site := t.TempDir()
	os.WriteFile(filepath.Join(site, "index.html"), []byte(`<html><body><main data-pagefind-body><h1>Hello</h1><p>Main road</p></main></body></html>`), 0o644)
	if err := RunPagefind(bin, site, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"pagefind/pagefind.js", "pagefind/pagefind-ui.js", "pagefind/pagefind-entry.json"} {
		if _, err := os.Stat(filepath.Join(site, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
}
