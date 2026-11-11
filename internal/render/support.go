package render

import (
	"html"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
)

// supportBlock renders a ```support block: PayPal buttons for the
// configured one-time amounts and monthly plans (site.support). The block's
// body is ignored.
func (r *Renderer) supportBlock(_ string, line int, ctx Context) (string, error) {
	sp := r.Support
	if sp == nil {
		r.Warn(ctx.Path, line, "support: no site.support in the config")
		return "", nil
	}
	var b strings.Builder
	b.WriteString("\n" + `<div class="support">` + "\n")
	row := func(title string, links [][2]string) {
		if len(links) == 0 {
			return
		}
		b.WriteString(`<h3>` + title + `</h3><ul class="support-amounts">`)
		for _, l := range links {
			b.WriteString(`<li><a href="` + html.EscapeString(l[1]) + `" target="_blank" rel="noopener">` + html.EscapeString(l[0]) + `</a></li>`)
		}
		b.WriteString("</ul>\n")
	}
	var once, monthly [][2]string
	for _, a := range sp.Amounts {
		once = append(once, [2]string{money(a, sp.Currency),
			"https://paypal.me/" + sp.PayPalMe + "/" + strconv.FormatFloat(a, 'f', -1, 64) + sp.Currency})
	}
	for _, m := range sp.Monthly {
		monthly = append(monthly, [2]string{money(m.Amount, sp.Currency) + "/mo",
			"https://www.paypal.com/webapps/billing/plans/subscribe?plan_id=" + url.QueryEscape(m.PlanID)})
	}
	row("One-time", once)
	if sp.PayPalClientID != "" && len(sp.Monthly) > 0 {
		monthlyForm(&b, sp, monthly)
	} else {
		row("Monthly", monthly)
	}
	b.WriteString("</div>\n")
	return b.String(), nil
}

// customIDMax is PayPal's limit on a subscription's custom_id.
const customIDMax = 127

// monthlyForm renders the monthly plans with a free-text towns box. The
// page script (app.js) turns the amount links into a choice and shows
// PayPal's subscribe button, which stores the towns on the subscription as
// its custom_id. Without the script, or if PayPal's fails to load, the
// links still subscribe, just without the towns.
func monthlyForm(b *strings.Builder, sp *config.Support, links [][2]string) {
	b.WriteString(`<h3>Monthly</h3><div class="support-monthly" data-paypal-client="` + html.EscapeString(sp.PayPalClientID) + `">`)
	b.WriteString(`<label class="support-towns-label" for="support-towns">Towns you'd like us to cover <span class="small-text">(optional; any town, even one we don't cover yet)</span></label>`)
	b.WriteString(`<input id="support-towns" class="support-towns" type="text" maxlength="` + strconv.Itoa(customIDMax) + `" autocomplete="off" placeholder="e.g. Worcester MA, Springfield IL">`)
	b.WriteString(`<ul class="support-amounts">`)
	for i, l := range links {
		b.WriteString(`<li><a href="` + html.EscapeString(l[1]) + `" target="_blank" rel="noopener" data-plan="` +
			html.EscapeString(sp.Monthly[i].PlanID) + `">` + html.EscapeString(l[0]) + `</a></li>`)
	}
	b.WriteString(`</ul><div class="support-paypal" hidden></div><p class="support-status" role="status" hidden></p></div>` + "\n")
}

// money labels an amount: "$5", "$2.50", "5 EUR".
func money(a float64, currency string) string {
	s := strconv.FormatFloat(a, 'f', -1, 64)
	if a != math.Trunc(a) {
		s = strconv.FormatFloat(a, 'f', 2, 64)
	}
	if currency == "USD" {
		return "$" + s
	}
	return s + " " + currency
}
