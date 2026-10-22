package html

import (
	"html"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/deweysasser/obsidian-publish-website/internal/render"
)

var (
	paraRe   = regexp.MustCompile(`(?s)<p>(.*?)</p>`)
	tagRe    = regexp.MustCompile(`<[^>]*>`)
	emOnlyRe = regexp.MustCompile(`(?s)^\s*<em>.*</em>\s*$`)
	ytRe     = regexp.MustCompile(`class="yt[^"]*" data-yt="([A-Za-z0-9_-]{11})"`)
)

// describe returns a page's search/sharing description: its "description" or
// "summary" frontmatter, else its first substantial paragraph (skipping
// short ones, all-italic disclaimers and lead-ins ending in a colon).
func describe(p *render.Page) string {
	for _, k := range []string{"description", "summary"} {
		if s, ok := p.Frontmatter[k].(string); ok && strings.TrimSpace(s) != "" {
			return clip(strings.Join(strings.Fields(s), " "))
		}
	}
	for _, m := range paraRe.FindAllStringSubmatch(p.HTML, 50) {
		if emOnlyRe.MatchString(m[1]) {
			continue
		}
		text := strings.Join(strings.Fields(html.UnescapeString(tagRe.ReplaceAllString(m[1], ""))), " ")
		if len(text) >= 40 && !strings.HasSuffix(text, ":") { // skip lead-ins to a list or table
			return clip(text)
		}
	}
	return ""
}

func clip(s string) string {
	const max = 160
	if len(s) <= max {
		return s
	}
	cut := strings.LastIndex(s[:max], " ")
	if cut < max/2 {
		for cut = max; !utf8.RuneStart(s[cut]); cut-- {
		}
	}
	return strings.TrimRight(s[:cut], " ,.;:") + "…"
}

// shareImage is the thumbnail of the page's first YouTube video, if any.
func shareImage(pageHTML string) string {
	if m := ytRe.FindStringSubmatch(pageHTML); m != nil {
		return "https://i.ytimg.com/vi/" + m[1] + "/hqdefault.jpg"
	}
	return ""
}
