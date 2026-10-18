package render

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/deweysasser/obsidian-publish-website/internal/index"
	"github.com/deweysasser/obsidian-publish-website/internal/mdscan"
)

var (
	highlightRe = regexp.MustCompile(`==([^=\s](?:[^=]*[^=\s])?)==`)
	placeRe     = regexp.MustCompile("\x00(\\d+)\x00")
	lineKeyRe   = regexp.MustCompile(`^(\s*(?:[-*+]\s+(?:\[.\]\s+)?)?)(?:\*\*|__|\*|_)?([^:*_\[\]()\s][^:*\[\]()]*?)(?:\*\*|__|\*|_)?::\s*`)
	mdEscaper   = strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`, `*`, `\*`, `_`, `\_`, "`", "\\`", `<`, `\<`)
)

// Preprocess rewrites an Obsidian markdown body into target-flavored markdown.
func (r *Renderer) Preprocess(body []byte, firstLine int, ctx Context) (string, error) {
	lines := mdscan.StripComments(mdscan.Lines(body, firstLine))
	var out []string
	for i := 0; i < len(lines); i++ {
		ln := lines[i]
		switch {
		case ln.FenceOpen:
			j := i + 1
			for j < len(lines) && !lines[j].FenceClose {
				j++
			}
			end := min(j, len(lines)-1)
			if hook := r.fenceHooks[ln.Lang]; hook != nil {
				var code []string
				for _, l := range lines[i+1 : min(j, len(lines))] {
					code = append(code, strings.TrimPrefix(l.Text, ln.Quote))
				}
				res, err := hook(strings.Join(code, "\n"), ln.Num, ctx)
				if err != nil {
					return "", err
				}
				out = append(out, ln.Quote, prefixLines(res, ln.Quote), ln.Quote)
			} else {
				for _, l := range lines[i : end+1] {
					out = append(out, l.Text)
				}
			}
			i = end
		case ln.InFence:
			out = append(out, ln.Text)
		case strings.TrimSpace(strings.TrimPrefix(ln.Text, ln.Quote)) == "$$":
			j := i + 1
			for j < len(lines) && strings.TrimSpace(strings.TrimPrefix(lines[j].Text, ln.Quote)) != "$$" {
				j++
			}
			if j == len(lines) || r.Mode == ModeMarkdown {
				out = append(out, ln.Text)
				continue
			}
			var m []string
			for _, l := range lines[i+1 : j] {
				m = append(m, html.EscapeString(strings.TrimPrefix(l.Text, ln.Quote)))
			}
			out = append(out, ln.Quote+`<div class="math">\[`, prefixLines(strings.Join(m, "\n"), ln.Quote), ln.Quote+`\]</div>`)
			i = j
		case ln.Quote != "" && calloutRe.MatchString(ln.Text):
			res, end, err := r.callout(lines, i, ctx)
			if err != nil {
				return "", err
			}
			out = append(out, res)
			i = end
		default:
			out = append(out, r.rewriteLine(ln, ctx))
		}
	}
	s := strings.Join(out, "\n")
	if strings.HasSuffix(string(body), "\n") && len(out) > 0 {
		s += "\n"
	}
	return s, nil
}

func prefixLines(s, prefix string) string {
	if prefix == "" {
		return s
	}
	ls := strings.Split(s, "\n")
	for i := range ls {
		ls[i] = prefix + ls[i]
	}
	return strings.Join(ls, "\n")
}

// rewriteLine rewrites one prose line.
func (r *Renderer) rewriteLine(ln mdscan.Line, ctx Context) string {
	segs := mdscan.Inline(ln.Text)
	var b strings.Builder
	for i, seg := range segs {
		switch {
		case seg.Math:
			b.WriteString(r.math(seg.Text))
		case seg.Code:
			b.WriteString(r.inlineCode(seg.Text, ln.Num, ctx))
		default:
			text := seg.Text
			if i == 0 && !index.BracketFieldRe.MatchString(text) && !index.ParenFieldRe.MatchString(text) {
				if m := lineKeyRe.FindStringSubmatchIndex(text); m != nil && !strings.Contains(text[:m[1]], "://") {
					text = text[:m[2]] + text[m[2]:m[3]] + "**" + text[m[4]:m[5]] + "**: " + text[m[1]:]
				}
			}
			b.WriteString(r.rewriteProse(text, ln.Num, ctx, i == len(segs)-1))
		}
	}
	return b.String()
}

func (r *Renderer) math(src string) string {
	if r.Mode == ModeMarkdown {
		return src
	}
	if strings.HasPrefix(src, "$$") {
		return `<span class="math display">\[` + html.EscapeString(src[2:len(src)-2]) + `\]</span>`
	}
	return `<span class="math">\(` + html.EscapeString(src[1:len(src)-1]) + `\)</span>`
}

// inlineCode handles inline code spans; dataview inline queries hook in here.
func (r *Renderer) inlineCode(src string, line int, ctx Context) string {
	if r.inlineHook != nil {
		if out, ok := r.inlineHook(src, line, ctx); ok {
			return out
		}
	}
	return src
}

func (r *Renderer) rewriteProse(s string, line int, ctx Context, last bool) string {
	var places []string
	hold := func(v string) string {
		places = append(places, v)
		return "\x00" + strconv.Itoa(len(places)-1) + "\x00"
	}
	s = index.WikiRe.ReplaceAllStringFunc(s, func(m string) string {
		embed := strings.HasPrefix(m, "!")
		inner := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(m, "!"), "[["), "]]")
		l := index.ParseWikilink(inner, embed)
		return hold(r.link(l, line, ctx))
	})
	s = index.MdLinkRe.ReplaceAllStringFunc(s, func(m string) string {
		ls := index.LinksInLine(m, line)
		if len(ls) != 1 {
			return hold(m) // external URL or anchor: keep as written
		}
		return hold(r.link(ls[0], line, ctx))
	})
	s = highlightRe.ReplaceAllString(s, "<mark>$1</mark>")
	s = index.TagRe.ReplaceAllStringFunc(s, func(m string) string {
		lead := m[:len(m)-len(strings.TrimLeft(m, " \t"))]
		tags := index.TagsInLine(m)
		if len(tags) != 1 {
			return m
		}
		return lead + hold(fmt.Sprintf(`<a class="tag" href="%s">%s</a>`, html.EscapeString(r.URLs.TagURL(tags[0])), html.EscapeString(tags[0])))
	})
	s = index.BracketFieldRe.ReplaceAllString(s, `<span class="inline-field"><span class="if-key">$1</span><span class="if-val">$2</span></span>`)
	s = index.ParenFieldRe.ReplaceAllString(s, `<span class="inline-field"><span class="if-val">$2</span></span>`)
	if last {
		if m := index.BlockIDRe.FindStringSubmatchIndex(s); m != nil {
			id := s[m[2]:m[3]]
			s = s[:m[2]-1] + fmt.Sprintf(`<span class="block-id" id="block-%s"></span>`, html.EscapeString(id)) + s[m[1]:]
		}
	}
	for placeRe.MatchString(s) {
		s = placeRe.ReplaceAllStringFunc(s, func(m string) string {
			n, _ := strconv.Atoi(strings.Trim(m, "\x00"))
			return places[n]
		})
	}
	return s
}

// link renders a (non-embed or embed) link found in prose.
func (r *Renderer) link(l index.Link, line int, ctx Context) string {
	if l.Target == "" {
		l.Path = ctx.Path
	} else {
		l.Path, _ = r.Ix.Resolve(l.Target, ctx.Path)
	}
	if l.Embed && r.embedHook != nil {
		return r.embedHook(l, line, ctx)
	}
	res := r.Res.Link(l, ctx.Path)
	if l.Path == "" {
		r.Warn(ctx.Path, line, fmt.Sprintf("unresolved link %q", l.Raw))
	}
	if !res.Published {
		return mdEscaper.Replace(res.Text)
	}
	return "[" + mdEscaper.Replace(res.Text) + "](<" + res.URL + ">)"
}
