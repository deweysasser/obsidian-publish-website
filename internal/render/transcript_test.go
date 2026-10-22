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
	r := transcriptRenderer(t, "---\nurl: https://www.youtube.com/watch?v=abcdefghijk\n---\n")
	out, err := r.Preprocess([]byte("```transcript\n0:07 | Hi <there> [[x]]\n1:02:03 | later\n\nno stamp\n```\n"), 1, Context{Path: "Town/V.md"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<details class="transcript">`,
		`<p><a href="https://youtu.be/abcdefghijk?t=7">0:07</a>Hi &lt;there&gt; [[x]]</p>`,
		`<p><a href="https://youtu.be/abcdefghijk?t=3723">1:02:03</a>later</p>`,
		`<p><span></span>no stamp</p>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Count(out, `<p>`) != 3 {
		t.Errorf("blank lines should be dropped:\n%s", out)
	}
}

func TestTranscriptWithoutVideoHasPlainTimestamps(t *testing.T) {
	for _, fm := range []string{"", "---\nurl: https://example.com/v\n---\n", "---\nurl: javascript:alert(1)\n---\n"} {
		r := transcriptRenderer(t, fm)
		out, _ := r.Preprocess([]byte("```transcript\n0:07 | Hi\n```\n"), 1, Context{Path: "Town/V.md"})
		if strings.Contains(out, "<a ") || !strings.Contains(out, `<p><span>0:07</span>Hi</p>`) {
			t.Errorf("fm %q: %s", fm, out)
		}
	}
}

func TestTranscriptYouTubeShortLink(t *testing.T) {
	for _, u := range []string{"https://youtu.be/abcdefghijk", "https://www.youtube.com/live/abcdefghijk?si=x", "https://m.youtube.com/watch?v=abcdefghijk&t=10s"} {
		r := transcriptRenderer(t, "---\nvideo: "+u+"\n---\n")
		out, _ := r.Preprocess([]byte("```transcript\n[2:05] Hi\n```\n"), 1, Context{Path: "Town/V.md"})
		if !strings.Contains(out, `href="https://youtu.be/abcdefghijk?t=125">2:05</a>`) {
			t.Errorf("%s: %s", u, out)
		}
	}
}

func TestTranscriptUnrecognizedYouTubeURLKeepsFullLink(t *testing.T) {
	r := transcriptRenderer(t, "---\nvideo: https://www.youtube.com/playlist?list=PL1\n---\n")
	out, _ := r.Preprocess([]byte("```transcript\n0:05 Hi\n```\n"), 1, Context{Path: "Town/V.md"})
	if !strings.Contains(out, `href="https://www.youtube.com/playlist?list=PL1&amp;t=5s">0:05</a>`) {
		t.Errorf("%s", out)
	}
}

func TestYouTubeImageEmbedsPlayer(t *testing.T) {
	r, _ := fixtureRenderer(t)
	out, _, err := r.MarkdownToHTML("![Watch the video](https://www.youtube.com/watch?v=abcdefghijk)\n\n![pic](a.png)\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<span class="yt" data-yt="abcdefghijk">`,
		`<a href="https://www.youtube.com/watch?v=abcdefghijk">`,
		`<img src="https://i.ytimg.com/vi/abcdefghijk/hqdefault.jpg" alt="Watch the video" loading="lazy">`,
		`<img src="a.png" alt="pic">`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestNonEmbeddableVideosAreMarked(t *testing.T) {
	r := transcriptRenderer(t, "---\nurl: https://youtu.be/abcdefghijk\n---\n")
	r.YouTubeEmbeddable = func(id string) bool { return id != "abcdefghijk" }
	out, _ := r.Preprocess([]byte("```transcript\n0:05 Hi\n```\n"), 1, Context{Path: "Town/V.md"})
	if !strings.Contains(out, `<details class="transcript" data-noembed>`) {
		t.Errorf("transcript: %s", out)
	}
	html, _, _ := r.MarkdownToHTML("![v](https://youtu.be/abcdefghijk)\n\n![w](https://youtu.be/zzzzzzzzzzz)\n")
	if !strings.Contains(html, `<span class="yt noembed" data-yt="abcdefghijk">`) || !strings.Contains(html, `<span class="yt" data-yt="zzzzzzzzzzz">`) {
		t.Errorf("embeds: %s", html)
	}
}
