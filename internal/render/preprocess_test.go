package render

import (
	"strings"
	"testing"
)

func TestPreprocess(t *testing.T) {
	cases := []struct{ in, want string }{
		{"[[Road Repair]]", "[Road Repair](</c/issues/road-repair/>)"},
		{"[[Road Repair|the road]]", "[the road](</c/issues/road-repair/>)"},
		{"[[Road Repair#Context]]", "[Road Repair > Context](</c/issues/road-repair/#context>)"},
		{"[[Alice Smith]] and [[Bob (Elm St)|Bob]]", "Alice Smith and Bob"},
		{"==hi== there", "<mark>hi</mark> there"},
		{"a %%secret%% b", "a  b"},
		{"a\n%%\nsecret\n%%\nb", "a\nb"},
		{"x #civic/roads y", `x <a class="tag" href="/c/tags/civic-roads/">#civic/roads</a> y`},
		{"see [due:: 2026-10-01]", `see <span class="inline-field"><span class="if-key">due</span><span class="if-val">2026-10-01</span></span>`},
		{"see (due:: 2026-10-01)", `see <span class="inline-field"><span class="if-val">2026-10-01</span></span>`},
		{"priority:: 2", "**priority**: 2"},
		{"[Road](Issues/Road%20Repair.md)", "[Road](</c/issues/road-repair/>)"},
		{"[x](https://e.com)", "[x](https://e.com)"},
		{"para ^blk9", `para <span class="block-id" id="block-blk9"></span>`},
		{"[[Café Permit]]", "[Café Permit](</c/issues/caf%C3%A9-permit/>)"},
		{"[[Road Repair|a *c* <b>]]", `[a \*c\* \<b>](</c/issues/road-repair/>)`},
		{"[[#Context]]", "[Context](</c/#context>)"},
	}
	for _, c := range cases {
		r, _ := fixtureRenderer(t)
		if got := pre(t, r, c.in); got != c.want {
			t.Errorf("Preprocess(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestPreprocessUnresolvedWarns(t *testing.T) {
	r, warns := fixtureRenderer(t)
	if got := pre(t, r, "x\n[[Nope]]"); got != "x\nNope" {
		t.Errorf("got %q", got)
	}
	if len(*warns) != 1 || (*warns)[0].line != 2 || !strings.Contains((*warns)[0].msg, "Nope") {
		t.Errorf("warnings %+v", *warns)
	}
	r, warns = fixtureRenderer(t)
	pre(t, r, "[[Alice Smith]]")
	if len(*warns) != 0 {
		t.Errorf("unpublished link should not warn: %+v", *warns)
	}
}

func TestPreprocessSkipsCode(t *testing.T) {
	src := "```\n[[Road Repair]] ==y== %%c%% #tag\n> [!note]\n```\n~~~\n[[Road Repair]]\n~~~\n`[[Road Repair]] ==y== #tag %%c%%` done\n"
	r, _ := fixtureRenderer(t)
	if got := pre(t, r, src); got != src {
		t.Errorf("code rewritten:\n%s", got)
	}
}

func TestPreprocessMath(t *testing.T) {
	r, _ := fixtureRenderer(t)
	got := pre(t, r, "e $x<1$ f\n$$\na<b\n$$\n")
	want := "e <span class=\"math\">\\(x&lt;1\\)</span> f\n<div class=\"math\">\\[\na&lt;b\n\\]</div>\n"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	r, _ = newTestRenderer(t, "../../testdata/fixture-vault", ModeMarkdown)
	if got := pre(t, r, "e $x<1$ f"); got != "e $x<1$ f" {
		t.Errorf("markdown mode math %q", got)
	}
}

func TestPreprocessFenceHook(t *testing.T) {
	r, _ := fixtureRenderer(t)
	var gotCode string
	var gotLine int
	r.fenceHooks["dataview"] = func(code string, line int, ctx Context) (string, error) {
		gotCode, gotLine = code, line
		return "<div>RESULT</div>", nil
	}
	got := pre(t, r, "a\n\n```dataview\nLIST\nFROM \"x\"\n```\nb")
	if got != "a\n\n\n<div>RESULT</div>\n\nb" || gotCode != "LIST\nFROM \"x\"" || gotLine != 3 {
		t.Errorf("got %q code %q line %d", got, gotCode, gotLine)
	}
}

func TestCommentsFailClosedAroundCode(t *testing.T) {
	for in, forbidden := range map[string]string{
		"`%%` shown %% secret":          "secret",
		"a %% hidden `%%` hidden2 %% b": "hidden2",
		"%% start\n`x` secret\n%%":      "secret",
	} {
		r, _ := fixtureRenderer(t)
		if got := pre(t, r, in); strings.Contains(got, forbidden) {
			t.Errorf("Preprocess(%q) leaked %q: %q", in, forbidden, got)
		}
	}
}

func TestCommentInsideMathSpanHides(t *testing.T) {
	r, _ := fixtureRenderer(t)
	if got := pre(t, r, "price $5 %%secret$ hidden%% end"); strings.Contains(got, "secret") || strings.Contains(got, "hidden") {
		t.Errorf("leaked: %q", got)
	}
}
