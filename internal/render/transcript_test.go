package render

import (
	"strings"
	"testing"
)

func TestSoftBreaksFollowObsidianSetting(t *testing.T) {
	r, _ := fixtureRenderer(t)
	out, _, err := r.MarkdownToHTML("a\nb\n")
	if err != nil || !strings.Contains(out, "a<br") {
		t.Errorf("default (non-strict) should keep line breaks: %q %v", out, err)
	}
	r.StrictLineBreaks = true
	out, _, _ = r.MarkdownToHTML("a\nb\n")
	if strings.Contains(out, "<br") {
		t.Errorf("strict line breaks should join lines: %q", out)
	}
}

func transcriptRenderer(t *testing.T, frontmatter string) *Renderer {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "Town/Town.md", "home")
	writeFile(t, dir, "Town/V.md", frontmatter+"x")
	r, _ := newTestRenderer(t, dir, ModeHTML)
	return r
}

func TestTranscriptYouTubeTimestamps(t *testing.T) {
	r := transcriptRenderer(t, "---\nurl: https://www.youtube.com/watch?v=abc\n---\n")
	out, err := r.Preprocess([]byte("```transcript\n0:07 | Hi <there> [[x]]\n1:02:03 | later\n\nno stamp\n```\n"), 1, Context{Path: "Town/V.md"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<details class="transcript">`,
		`<a class="ts" href="https://www.youtube.com/watch?v=abc&amp;t=7s">0:07</a>`,
		`Hi &lt;there&gt; [[x]]`,
		`href="https://www.youtube.com/watch?v=abc&amp;t=3723s">1:02:03</a>`,
		`<span class="ts"></span><span class="tx">no stamp</span>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Count(out, `class="tl"`) != 3 {
		t.Errorf("blank lines should be dropped:\n%s", out)
	}
}

func TestTranscriptWithoutVideoHasPlainTimestamps(t *testing.T) {
	for _, fm := range []string{"", "---\nurl: https://example.com/v\n---\n", "---\nurl: javascript:alert(1)\n---\n"} {
		r := transcriptRenderer(t, fm)
		out, _ := r.Preprocess([]byte("```transcript\n0:07 | Hi\n```\n"), 1, Context{Path: "Town/V.md"})
		if strings.Contains(out, "<a ") || !strings.Contains(out, `<span class="ts">0:07</span>`) {
			t.Errorf("fm %q: %s", fm, out)
		}
	}
}

func TestTranscriptYouTubeShortLink(t *testing.T) {
	r := transcriptRenderer(t, "---\nvideo: https://youtu.be/abc\n---\n")
	out, _ := r.Preprocess([]byte("```transcript\n[2:05] Hi\n```\n"), 1, Context{Path: "Town/V.md"})
	if !strings.Contains(out, `href="https://youtu.be/abc?t=125s">2:05</a>`) {
		t.Errorf("%s", out)
	}
}
