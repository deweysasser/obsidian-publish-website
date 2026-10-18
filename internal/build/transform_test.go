package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/render"
	"github.com/deweysasser/obsidian-publish-website/internal/transform"
	"github.com/rs/zerolog"
)

type upperTitle struct{}

func (upperTitle) Name() string { return "upper-title" }
func (upperTitle) Apply(p *render.Page) error {
	p.Title = strings.ToUpper(p.Title)
	return nil
}

func TestTransformsApplied(t *testing.T) {
	transform.Register("upper-title", func() transform.Transform { return upperTitle{} })
	out := t.TempDir()
	cfg := fixtureConfig(out, "html")
	cfg.Transforms = []string{"upper-title"}
	if _, err := Run(cfg, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, filepath.Join(out, "issues/road-repair/index.html")), "<title>ROAD REPAIR · T</title>") {
		t.Error("transform not applied")
	}
}

func TestUnknownTransformFailsBeforeOutput(t *testing.T) {
	out := filepath.Join(t.TempDir(), "o")
	cfg := fixtureConfig(out, "html")
	cfg.Transforms = []string{"no-such-transform"}
	if _, err := Run(cfg, zerolog.Nop()); err == nil || !strings.Contains(err.Error(), "no-such-transform") {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("output written")
	}
}

type redactContext struct{}

func (redactContext) Name() string { return "redact-context" }
func (redactContext) Apply(p *render.Page) error {
	p.Markdown = strings.ReplaceAll(p.Markdown, "Context", "REDACTED")
	return nil
}

func TestTransformsFeedHTMLAndTOC(t *testing.T) {
	transform.Register("redact-context", func() transform.Transform { return redactContext{} })
	out := t.TempDir()
	cfg := fixtureConfig(out, "html")
	cfg.Transforms = []string{"redact-context"}
	if _, err := Run(cfg, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	page := read(t, filepath.Join(out, "issues/road-repair/index.html"))
	if strings.Contains(page, "Context") || !strings.Contains(page, `href="#redacted"`) {
		t.Errorf("transform did not reach HTML/TOC:\n%s", page)
	}
}
