package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func tinyVault(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(dir, "R", rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	return dir
}

func buildTiny(t *testing.T, files map[string]string) (string, error) {
	out := filepath.Join(t.TempDir(), "o")
	cfg := fixtureConfig(out, "html")
	cfg.Vault, cfg.Root, cfg.Exclude = tinyVault(t, files), "R", nil
	_, err := Run(cfg, zerolog.Nop())
	return out, err
}

func TestReservedSlugRejected(t *testing.T) {
	for _, name := range []string{"Tags.md", "All.md", "assets/x.md", "Pagefind.md"} {
		if _, err := buildTiny(t, map[string]string{"a.md": "a", name: "x"}); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("%s: want reserved-slug error, got %v", name, err)
		}
	}
}

func TestNote404NotOverwritten(t *testing.T) {
	out, err := buildTiny(t, map[string]string{"404.md": "FOUR OH FOUR NOTE"})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "404/index.html")); !strings.Contains(string(b), "FOUR OH FOUR NOTE") {
		t.Errorf("404 note lost:\n%s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "404.html")); strings.Contains(string(b), "FOUR OH FOUR NOTE") {
		t.Error("not-found page replaced by note")
	}
}

func TestAssetSlugCollision(t *testing.T) {
	out, err := buildTiny(t, map[string]string{"a.md": "![[A b.png]] ![[a-b.png]]", "A b.png": "ONE", "a-b.png": "TWO"})
	if err != nil {
		t.Fatal(err)
	}
	page, _ := os.ReadFile(filepath.Join(out, "a/index.html"))
	srcs := regexpAll(`<img src="([^"]+)"`, string(page))
	if len(srcs) != 2 || srcs[0] == srcs[1] {
		t.Fatalf("srcs %v", srcs)
	}
	for _, s := range srcs {
		b, _ := os.ReadFile(filepath.Join(out, strings.TrimPrefix(s, "/c/")))
		if len(b) == 0 {
			t.Errorf("missing %s", s)
		}
	}
}

func TestTagCaseVariantsShareAPage(t *testing.T) {
	out, err := buildTiny(t, map[string]string{"a.md": "#Project", "b.md": "#project"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(out, "tags/project/index.html"))
	if !strings.Contains(string(b), ">a<") || !strings.Contains(string(b), ">b<") {
		t.Errorf("tag page should list both:\n%s", b)
	}
}

func regexpAll(re, s string) []string {
	var out []string
	for _, m := range regexp.MustCompile(re).FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}
