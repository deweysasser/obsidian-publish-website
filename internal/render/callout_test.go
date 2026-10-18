package render

import (
	"strings"
	"testing"
)

func TestCallouts(t *testing.T) {
	r, _ := fixtureRenderer(t)
	got := pre(t, r, "> [!note] Hi [[Road Repair]]\n> body [[Road Repair]]\n\nafter")
	want := "<div class=\"callout\" data-callout=\"note\">\n<div class=\"callout-title\">\n\nHi [Road Repair](/c/issues/road-repair/)\n\n</div>\n<div class=\"callout-content\">\n\nbody [Road Repair](/c/issues/road-repair/)\n\n</div>\n</div>\n\nafter"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestCalloutFoldableAliasDefaultTitle(t *testing.T) {
	r, _ := fixtureRenderer(t)
	got := pre(t, r, "> [!faq]- \n> a")
	if !strings.HasPrefix(got, "<details class=\"callout\" data-callout=\"question\">\n<summary class=\"callout-title\">\n\nFaq\n\n</summary>") || strings.Contains(got, " open") || !strings.HasSuffix(got, "</details>") {
		t.Errorf("got\n%s", got)
	}
	got = pre(t, r, "> [!TIP]+ Open me\n> a")
	if !strings.HasPrefix(got, "<details class=\"callout\" data-callout=\"tip\" open>") {
		t.Errorf("got\n%s", got)
	}
}

func TestCalloutNestedAndPlainQuote(t *testing.T) {
	r, _ := fixtureRenderer(t)
	got := pre(t, r, "> [!note]\n> outer\n> > [!warning] W\n> > inner")
	if strings.Count(got, "class=\"callout\"") != 2 || !strings.Contains(got, `data-callout="warning"`) || strings.Contains(got, "> inner") {
		t.Errorf("got\n%s", got)
	}
	if got := pre(t, r, "> quote [[Road Repair]]"); got != "> quote [Road Repair](/c/issues/road-repair/)" {
		t.Errorf("plain quote %q", got)
	}
}

func TestCalloutWithFence(t *testing.T) {
	r, _ := fixtureRenderer(t)
	got := pre(t, r, "> [!info]\n> ```\n> [[x]]\n> ```")
	if !strings.Contains(got, "\n```\n[[x]]\n```\n") {
		t.Errorf("got\n%s", got)
	}
}
