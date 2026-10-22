package render

import (
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/deweysasser/obsidian-publish-website/internal/youtube"
)

// transcriptLineRe matches "0:07 | text", "1:02:03 text" and "[2:05] text".
var transcriptLineRe = regexp.MustCompile(`^\[?((?:\d+:)?\d{1,2}:\d{2})\]?\s*(?:[|–-]\s*)?(.*)$`)

// transcriptBlock renders a ```transcript block as a collapsible timeline.
// Text is literal (as Obsidian shows a code block). Timestamps link into the
// note's video when its "video" or "url" frontmatter is a YouTube URL.
func (r *Renderer) transcriptBlock(code string, _ int, ctx Context) (string, error) {
	video := r.videoURL(ctx.Path)
	id := youtube.ID(video)
	var b strings.Builder
	b.WriteString("<details class=\"transcript\"")
	if id != "" && !r.embeddable(id) {
		b.WriteString(" data-noembed") // timestamps open YouTube rather than a player
	}
	b.WriteString(">\n<summary>Transcript</summary>\n<div class=\"transcript-lines\">\n")
	for _, line := range strings.Split(code, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		stamp, text := "", line
		if m := transcriptLineRe.FindStringSubmatch(line); m != nil {
			stamp, text = m[1], m[2]
		}
		// Markup is deliberately terse: a long meeting has thousands of lines.
		b.WriteString("<p>")
		switch {
		case stamp != "" && id != "":
			b.WriteString(`<a href="https://youtu.be/` + id + "?t=" + strconv.Itoa(seconds(stamp)) + `">` + stamp + `</a>`)
		case stamp != "" && video != nil:
			b.WriteString(`<a href="` + html.EscapeString(withTime(video, seconds(stamp))) + `">` + stamp + `</a>`)
		default:
			b.WriteString(`<span>` + stamp + `</span>`)
		}
		b.WriteString(html.EscapeString(text) + "</p>\n")
	}
	b.WriteString("</div>\n</details>\n")
	return "\n" + b.String(), nil
}

func (r *Renderer) videoURL(notePath string) *url.URL {
	pg := r.Ix.Pages[notePath]
	if pg == nil {
		return nil
	}
	for _, key := range []string{"video", "url"} {
		s, _ := pg.Note.Frontmatter[key].(string)
		u, err := url.Parse(strings.TrimSpace(s))
		if err != nil || u.Scheme != "https" && u.Scheme != "http" {
			continue
		}
		switch strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.") {
		case "youtube.com", "m.youtube.com", "youtu.be":
			return u
		}
	}
	return nil
}

func withTime(u *url.URL, secs int) string {
	c := *u
	if q := c.Query(); q.Has("t") {
		q.Del("t")
		c.RawQuery = q.Encode()
	}
	t := "t=" + strconv.Itoa(secs) + "s"
	if c.RawQuery == "" {
		c.RawQuery = t
	} else {
		c.RawQuery += "&" + t
	}
	return c.String()
}

func seconds(stamp string) int {
	total := 0
	for _, part := range strings.Split(stamp, ":") {
		n, _ := strconv.Atoi(part)
		total = total*60 + n
	}
	return total
}
