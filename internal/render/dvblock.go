package render

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/deweysasser/obsidian-publish-website/internal/dataview"
)

// dvState holds dataview evaluation state for a renderer.
type dvState struct {
	collecting bool
	disabled   bool
	requests   []dataview.Request
	ids        map[string]string // key → request id
	results    map[string]dataview.Result
	settings   map[string]any
}

const unsupportedMsg = "Dynamic content not available in published version"

func dvKey(kind, origin, source string) string { return kind + "\x00" + origin + "\x00" + source }

// CollectQueries walks every published page (and what it embeds) and returns
// the dataview queries and inline expressions that rendering will need.
func (r *Renderer) CollectQueries() ([]dataview.Request, error) {
	r.dv.collecting = true
	r.dv.requests = nil
	r.dv.ids = map[string]string{}
	defer func() { r.dv.collecting = false }()
	warn := r.Warn
	r.Warn = func(string, int, string) {} // warnings are reported by the real render
	defer func() { r.Warn = warn }()
	for _, p := range r.Ix.Paths {
		if !r.Pub.IsPublished(p) {
			continue
		}
		n := r.Ix.Pages[p].Note
		if _, err := r.Preprocess(n.Body, n.BodyLine, Context{Path: p}); err != nil {
			return nil, err
		}
	}
	return r.dv.requests, nil
}

// SetDataview installs evaluated results.
func (r *Renderer) SetDataview(results map[string]dataview.Result, settings map[string]any) {
	r.dv.results = results
	r.dv.settings = settings
}

// DisableDataview renders every query as a placeholder (dataview: none).
func (r *Renderer) DisableDataview() { r.dv.disabled = true }

func (r *Renderer) dvRequest(kind, origin, source string) (dataview.Result, bool) {
	key := dvKey(kind, origin, source)
	if r.dv.collecting {
		if _, ok := r.dv.ids[key]; !ok {
			id := fmt.Sprintf("q%d", len(r.dv.requests))
			r.dv.ids[key] = id
			r.dv.requests = append(r.dv.requests, dataview.Request{ID: id, Kind: kind, Origin: origin, Source: source})
		}
		return dataview.Result{}, false
	}
	res, ok := r.dv.results[r.dv.ids[key]]
	return res, ok
}

func (r *Renderer) dvBlock(code string, line int, ctx Context) (string, error) {
	if r.dv.disabled {
		return `<div class="dv-unsupported">` + unsupportedMsg + `</div>`, nil
	}
	res, ok := r.dvRequest("query", ctx.Path, code)
	if r.dv.collecting {
		return "", nil
	}
	if !ok {
		r.Warn(ctx.Path, line, "dataview query was not evaluated")
		return `<div class="dv-error">Dataview: query was not evaluated</div>`, nil
	}
	if !res.OK {
		if res.Unsupported {
			r.Warn(ctx.Path, line, "dataview: "+res.Error)
			return `<div class="dv-unsupported">` + unsupportedMsg + `</div>`, nil
		}
		r.Warn(ctx.Path, line, "dataview query failed: "+firstLine(res.Error))
		return `<div class="dv-error">Dataview: ` + html.EscapeString(strings.TrimSpace(res.Error)) + `</div>`, nil
	}
	return oneBlock(r.formatResult(res, ctx)), nil
}

func (r *Renderer) dvJS(_ string, line int, ctx Context) (string, error) {
	if !r.dv.collecting {
		r.Warn(ctx.Path, line, "dataviewjs is not supported; rendered as a placeholder")
	}
	return `<div class="dv-unsupported">` + unsupportedMsg + `</div>`, nil
}

var inlineCodeRe = regexp.MustCompile("^(`+)\\s?(.*?)\\s?(`+)$")

// dvInline handles inline `= expr` and `$= js` code spans.
func (r *Renderer) dvInline(src string, line int, ctx Context) (string, bool) {
	m := inlineCodeRe.FindStringSubmatch(src)
	if m == nil {
		return "", false
	}
	body := m[2]
	prefix := r.dvString("inlineQueryPrefix", "=")
	jsPrefix := r.dvString("inlineJsQueryPrefix", "$=")
	switch {
	case strings.HasPrefix(body, jsPrefix):
		if !r.dv.collecting && !r.dv.disabled {
			r.Warn(ctx.Path, line, "inline dataviewjs is not supported; rendered as a placeholder")
		}
		return `<span class="dv-unsupported">` + unsupportedMsg + `</span>`, true
	case strings.HasPrefix(body, prefix) && r.dvBool("enableInlineDataview", true):
		if r.dv.disabled {
			return `<span class="dv-unsupported">` + unsupportedMsg + `</span>`, true
		}
		expr := strings.TrimSpace(strings.TrimPrefix(body, prefix))
		res, ok := r.dvRequest("inline", ctx.Path, expr)
		if r.dv.collecting {
			return "", true
		}
		if !ok {
			r.Warn(ctx.Path, line, "dataview inline query was not evaluated")
			return `<span class="dv-error">Dataview: query was not evaluated</span>`, true
		}
		if !res.OK {
			r.Warn(ctx.Path, line, "dataview inline query failed: "+firstLine(res.Error))
			return `<span class="dv-error">Dataview: ` + html.EscapeString(firstLine(res.Error)) + `</span>`, true
		}
		return r.formatValue(res.Value, ctx), true
	}
	return "", false
}

func firstLine(s string) string {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "Error:"))
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[:i]
	}
	return s
}

// oneBlock makes an HTML island safe for markdown: a raw HTML block ends at
// the first blank line, so none may appear inside.
func oneBlock(s string) string {
	for strings.Contains(s, "\n\n") {
		s = strings.ReplaceAll(s, "\n\n", "\n")
	}
	return s
}

func (r *Renderer) dvString(key, def string) string {
	if v, ok := r.dv.settings[key].(string); ok {
		return v
	}
	return def
}

func (r *Renderer) dvBool(key string, def bool) bool {
	if v, ok := r.dv.settings[key].(bool); ok {
		return v
	}
	return def
}
