package render

import (
	"fmt"
	"html"
	"path"
	"strconv"
	"strings"

	"github.com/deweysasser/obsidian-publish-website/internal/dataview"
	"github.com/deweysasser/obsidian-publish-website/internal/index"
	"github.com/deweysasser/obsidian-publish-website/internal/mdscan"
)

func (r *Renderer) formatResult(res dataview.Result, ctx Context) string {
	var b strings.Builder
	count := ""
	n := 0
	switch res.Type {
	case "table":
		n = len(res.Rows)
	case "list":
		n = len(res.Items)
	case "task":
		for _, g := range res.TaskGroups {
			n += len(g.Tasks)
		}
	}
	if n == 0 && r.dvBool("warnOnEmptyResult", true) {
		return `<div class="dv-empty">Nothing right now.</div>`
	}
	if r.dvBool("showResultCount", true) {
		count = fmt.Sprintf(`<span class="dataview small-text">%d</span>`, n)
	}
	switch res.Type {
	case "table":
		b.WriteString(`<div class="dataview table-view"><table class="dataview"><thead><tr>`)
		for i, h := range res.Headers {
			b.WriteString("<th>" + html.EscapeString(h))
			if i == 0 {
				b.WriteString(count)
			}
			b.WriteString("</th>")
		}
		b.WriteString("</tr></thead><tbody>")
		for _, row := range res.Rows {
			b.WriteString("<tr>")
			for _, v := range row {
				b.WriteString("<td>" + r.formatValue(v, ctx) + "</td>")
			}
			b.WriteString("</tr>")
		}
		b.WriteString("</tbody></table></div>")
	case "list":
		b.WriteString(`<div class="dataview list-view"><ul class="dataview list-view-ul">`)
		for _, v := range res.Items {
			b.WriteString("<li>" + r.formatValue(v, ctx) + "</li>")
		}
		b.WriteString("</ul></div>")
	case "task":
		b.WriteString(`<div class="dataview task-view">`)
		for _, g := range res.TaskGroups {
			if g.Key != nil {
				b.WriteString("<h4>" + r.formatValue(*g.Key, ctx) + "</h4>")
				r.writeTasks(&b, g.Tasks)
				continue
			}
			// ungrouped tasks are shown under their file, as Dataview does
			var order []string
			byPath := map[string][]dataview.Task{}
			for _, t := range g.Tasks {
				if _, ok := byPath[t.Path]; !ok {
					order = append(order, t.Path)
				}
				byPath[t.Path] = append(byPath[t.Path], t)
			}
			for _, p := range order {
				b.WriteString("<h4>" + r.formatValue(dataview.Value{Kind: dataview.KLink, Link: dataview.Link{Path: p, Type: "file"}}, ctx) + "</h4>")
				r.writeTasks(&b, byPath[p])
			}
		}
		b.WriteString("</div>")
	}
	return b.String()
}

func (r *Renderer) writeTasks(b *strings.Builder, tasks []dataview.Task) {
	b.WriteString(`<ul class="contains-task-list">`)
	for _, t := range tasks {
		if t.Task {
			checked := ""
			if t.Checked {
				checked = " checked"
			}
			fmt.Fprintf(b, `<li class="task-list-item" data-task="%s"><input type="checkbox" disabled%s> `, html.EscapeString(t.Status), checked)
		} else {
			b.WriteString("<li>")
		}
		b.WriteString(r.inlineMarkdown(t.Text, Context{Path: t.Path}))
		if len(t.Children) > 0 {
			r.writeTasks(b, t.Children)
		}
		b.WriteString("</li>")
	}
	b.WriteString("</ul>")
}

// formatValue renders one Dataview value as inline HTML.
func (r *Renderer) formatValue(v dataview.Value, ctx Context) string {
	switch v.Kind {
	case dataview.KNull:
		return r.inlineMarkdown(r.dvString("renderNullAs", `\-`), ctx)
	case dataview.KBool:
		return strconv.FormatBool(v.Bool)
	case dataview.KNumber:
		return strconv.FormatFloat(v.Num, 'f', -1, 64)
	case dataview.KString, dataview.KText:
		return r.inlineMarkdown(v.Str, ctx)
	case dataview.KDate, dataview.KDuration:
		return html.EscapeString(v.Str)
	case dataview.KLink:
		return r.formatLink(v.Link)
	case dataview.KList:
		if len(v.List) == 0 {
			return ""
		}
		var b strings.Builder
		b.WriteString(`<ul class="dataview-list">`)
		for _, e := range v.List {
			b.WriteString("<li>" + r.formatValue(e, ctx) + "</li>")
		}
		b.WriteString("</ul>")
		return b.String()
	case dataview.KObject:
		var b strings.Builder
		b.WriteString(`<ul class="dataview-object">`)
		for _, k := range v.Keys {
			b.WriteString(`<li><span class="dv-key">` + html.EscapeString(k) + "</span>: " + r.formatValue(v.Obj[k], ctx) + "</li>")
		}
		b.WriteString("</ul>")
		return b.String()
	case dataview.KPair:
		return r.formatValue(*v.Pair[0], ctx) + ": " + r.formatValue(*v.Pair[1], ctx)
	case dataview.KExternalLink:
		text := v.Str
		if text == "" {
			text = v.URL
		}
		if !safeURL(v.URL) {
			return html.EscapeString(text)
		}
		return `<a class="external-link" href="` + html.EscapeString(v.URL) + `">` + html.EscapeString(text) + "</a>"
	}
	return ""
}

func safeURL(u string) bool {
	l := strings.ToLower(strings.TrimSpace(u))
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "mailto:")
}

// formatLink renders a Dataview link: published targets become links,
// everything else plain text.
func (r *Renderer) formatLink(l dataview.Link) string {
	base := strings.TrimSuffix(path.Base(l.Path), ".md")
	il := index.Link{Target: base, Display: l.Display, Embed: l.Embed}
	if l.Type == "header" || l.Type == "block" {
		il.Subpath = l.Subpath
		if l.Type == "block" && !strings.HasPrefix(il.Subpath, "^") {
			il.Subpath = "^" + il.Subpath
		}
	}
	if r.Ix.Pages[l.Path] != nil || isAttachment(r.Ix, l.Path) {
		il.Path = l.Path
	}
	if l.Embed && il.Path != "" && r.Ix.Pages[l.Path] == nil {
		return r.embedAsset(il, 0, Context{})
	}
	res := r.Res.Link(il, "")
	if !res.Published {
		return html.EscapeString(res.Text)
	}
	return `<a class="internal-link" href="` + html.EscapeString(res.URL) + `">` + html.EscapeString(res.Text) + "</a>"
}

func isAttachment(ix *index.Index, p string) bool {
	for _, a := range ix.Vault.Attachments {
		if a == p {
			return true
		}
	}
	return false
}

// inlineMarkdown renders a markdown fragment (field values, task text) to
// inline HTML with Obsidian syntax and link policy applied.
func (r *Renderer) inlineMarkdown(s string, ctx Context) string {
	if s == "" {
		return ""
	}
	var lines []string
	for i, l := range strings.Split(s, "\n") {
		lines = append(lines, r.rewriteLine(mdscan.Line{Text: l, Num: i + 1}, ctx))
	}
	out, _, err := r.MarkdownToHTML(strings.Join(lines, "\n"))
	if err != nil {
		return html.EscapeString(s)
	}
	out = strings.TrimSpace(out)
	if strings.HasPrefix(out, "<p>") && strings.HasSuffix(out, "</p>") && strings.Count(out, "<p>") == 1 {
		out = out[3 : len(out)-4]
	}
	return strings.ReplaceAll(out, "\n", " ")
}
