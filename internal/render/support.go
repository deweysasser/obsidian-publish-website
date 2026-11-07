package render

import (
	"html"
	"math"
	"net/url"
	"strconv"
	"strings"
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
	row("Monthly", monthly)
	b.WriteString("</div>\n")
	return b.String(), nil
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
