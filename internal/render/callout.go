package render

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/deweysasser/obsidian-publish-website/internal/mdscan"
)

var (
	calloutRe = regexp.MustCompile(`^\s{0,3}>\s?\[!([^\]\s]+)\]([+-]?)\s*(.*)$`)
	dequoteRe = regexp.MustCompile(`^\s{0,3}>\s?`)
	quoteLnRe = regexp.MustCompile(`^\s{0,3}>`)
)

var calloutAliases = map[string]string{
	"summary": "abstract", "tldr": "abstract",
	"hint": "tip", "important": "tip",
	"check": "success", "done": "success",
	"help": "question", "faq": "question",
	"caution": "warning", "attention": "warning",
	"fail": "failure", "missing": "failure",
	"error": "danger",
	"cite":  "quote",
}

// callout renders the callout starting at lines[i] and returns the HTML island
// and the index of its last line.
func (r *Renderer) callout(lines []mdscan.Line, i int, ctx Context) (string, int, error) {
	m := calloutRe.FindStringSubmatch(lines[i].Text)
	kind := strings.ToLower(m[1])
	canon := kind
	if a, ok := calloutAliases[kind]; ok {
		canon = a
	}
	title := strings.TrimSpace(m[3])
	if title == "" {
		title = strings.ToUpper(kind[:1]) + kind[1:]
	}
	j := i + 1
	var inner []string
	for j < len(lines) && quoteLnRe.MatchString(lines[j].Text) {
		inner = append(inner, dequoteRe.ReplaceAllString(lines[j].Text, ""))
		j++
	}
	body := ""
	if len(inner) > 0 {
		var err error
		body, err = r.Preprocess([]byte(strings.Join(inner, "\n")), lines[i].Num+1, ctx)
		if err != nil {
			return "", 0, err
		}
	}
	titleMD := r.rewriteLine(mdscan.Line{Text: title, Num: lines[i].Num}, ctx)
	attr := fmt.Sprintf(`class="callout" data-callout="%s"`, canon)
	contentAttr, titleAttr := `class="callout-content"`, `class="callout-title"`
	if r.Mode == ModeMarkdown { // kramdown only parses markdown inside HTML with markdown="1"
		contentAttr += ` markdown="1"`
		titleAttr += ` markdown="1"`
	}
	var b strings.Builder
	if m[2] == "" {
		fmt.Fprintf(&b, "<div %s>\n<div %s>\n\n%s\n\n</div>\n", attr, titleAttr, titleMD)
	} else {
		open := ""
		if m[2] == "+" {
			open = " open"
		}
		fmt.Fprintf(&b, "<details %s%s>\n<summary %s>\n\n%s\n\n</summary>\n", attr, open, titleAttr, titleMD)
	}
	fmt.Fprintf(&b, "<div %s>\n\n%s\n\n</div>\n", contentAttr, strings.TrimRight(body, "\n"))
	if m[2] == "" {
		b.WriteString("</div>")
	} else {
		b.WriteString("</details>")
	}
	return b.String(), j - 1, nil
}
