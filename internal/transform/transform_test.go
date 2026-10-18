package transform

import (
	"strings"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/render"
)

type upper struct{}

func (upper) Name() string { return "upper" }
func (upper) Apply(p *render.Page) error {
	p.Title = strings.ToUpper(p.Title)
	return nil
}

func TestChainAndApply(t *testing.T) {
	Register("upper", func() Transform { return upper{} })
	ts, err := Chain([]string{"upper"})
	if err != nil {
		t.Fatal(err)
	}
	p := &render.Page{Title: "hi"}
	if err := Apply(ts, p); err != nil || p.Title != "HI" {
		t.Errorf("title %q err %v", p.Title, err)
	}
	if _, err := Chain([]string{"nope"}); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("unknown transform: %v", err)
	}
}
