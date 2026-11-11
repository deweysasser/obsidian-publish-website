package html

import (
	"html/template"
	"net/url"
	"strconv"
	"strings"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
)

// suggestSlug is where the suggestion form is published (site.suggest).
const suggestSlug = "suggest"

// suggestLinks are the links at the bottom of a page, one per kind of
// suggestion, each carrying the page's path so it travels with the
// suggestion.
func (t *Target) suggestLinks(pagePath string) []tagLink {
	sg := t.cfg.Site.Suggest
	links := make([]tagLink, 0, len(sg.Kinds))
	for _, k := range sg.Kinds {
		links = append(links, tagLink{k.Link,
			t.base + suggestSlug + "/?kind=" + url.QueryEscape(k.ID) + "&page=" + url.QueryEscape(pagePath)})
	}
	return links
}

// suggestForm renders the form: a kind selector when there is more than one
// kind, each kind's fields and message (only the first kind's are shown and
// enabled until suggest.js picks the kind from the query), then the
// built-in page, contact and Turnstile parts every kind shares.
func suggestForm(sg *config.Suggest) template.HTML {
	esc := template.HTMLEscapeString
	var b strings.Builder
	b.WriteString(`<form class="suggest-form" data-endpoint="` + esc(sg.Endpoint) + `" data-pagefind-ignore="all">` + "\n")
	b.WriteString(`<noscript><p class="suggest-noscript">This form needs JavaScript, for its anti-spam check. Please turn JavaScript on to send a suggestion.</p></noscript>` + "\n")
	if len(sg.Kinds) > 1 {
		b.WriteString(`<fieldset class="suggest-kinds"><legend>What would you like to do?</legend>`)
		for i, k := range sg.Kinds {
			checked := ""
			if i == 0 {
				checked = " checked"
			}
			b.WriteString(`<label><input type="radio" name="kind" value="` + esc(k.ID) + `"` + checked + `> ` + esc(k.Link) + `</label>`)
		}
		b.WriteString("</fieldset>\n")
	} else {
		b.WriteString(`<input type="hidden" name="kind" value="` + esc(sg.Kinds[0].ID) + `">` + "\n")
	}
	for i, k := range sg.Kinds {
		off := ""
		if i > 0 {
			off = " disabled"
		}
		hidden := ""
		if i > 0 {
			hidden = " hidden"
		}
		b.WriteString(`<div class="suggest-kind" data-kind="` + esc(k.ID) + `"` + hidden + `>`)
		b.WriteString(`<h2>` + esc(k.Title) + `</h2>`)
		for _, f := range k.Fields {
			id := "suggest-" + k.ID + "-" + f.Name
			req := ""
			if f.Required {
				req = " required"
			}
			b.WriteString(`<p><label for="` + esc(id) + `">` + esc(f.Label) + optional(!f.Required) + `</label>`)
			b.WriteString(`<input type="text" id="` + esc(id) + `" name="` + esc(f.Name) + `" maxlength="` +
				strconv.Itoa(f.MaxLength) + `"` + req + off + `></p>`)
		}
		if k.Message != "none" {
			id := "suggest-" + k.ID + "-message"
			req := ""
			if k.Message == "required" {
				req = " required"
			}
			b.WriteString(`<p><label for="` + esc(id) + `">` + esc(k.MessageLabel) + optional(k.Message == "optional") + `</label>`)
			b.WriteString(`<textarea id="` + esc(id) + `" name="message" maxlength="5000" rows="6"` + req + off + `></textarea></p>`)
		}
		b.WriteString("</div>\n")
	}
	b.WriteString(`<p class="suggest-page" hidden><label for="suggest-page">Page</label>` +
		`<input type="text" id="suggest-page" name="page" maxlength="300" readonly></p>` + "\n")
	b.WriteString(`<p><label for="suggest-contact">Contact` + optional(true) + `</label>` +
		`<input type="text" id="suggest-contact" name="contact" maxlength="200" autocomplete="email">` +
		`<span class="hint">Only if you'd like a reply. It's never published.</span></p>` + "\n")
	b.WriteString(`<div class="suggest-turnstile"></div>` + "\n")
	b.WriteString(`<p><button type="submit">Send</button></p>` + "\n")
	b.WriteString(`<p class="suggest-status" role="status" hidden></p>` + "\n")
	b.WriteString("</form>")
	return template.HTML(b.String()) // #nosec G203 -- every interpolated value is escaped above
}

func optional(yes bool) string {
	if yes {
		return ` <span class="hint-inline">(optional)</span>`
	}
	return ""
}

// suggestTitle is the form page's heading: the kind's title when there is
// one kind, a neutral word when the visitor chooses.
func suggestTitle(sg *config.Suggest) string {
	if len(sg.Kinds) == 1 {
		return sg.Kinds[0].Title
	}
	return "Suggestions"
}
