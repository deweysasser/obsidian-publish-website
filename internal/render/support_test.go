package render

import (
	"strings"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
)

func TestSupportBlock(t *testing.T) {
	r := &Renderer{Warn: func(string, int, string) {}}
	r.Support = &config.Support{PayPalMe: "ExampleName", Currency: "USD", Amounts: []float64{1, 2.5},
		Monthly: []config.MonthlyPlan{{Amount: 5, PlanID: "P-5AB"}}}
	got, err := r.supportBlock("", 1, Context{Path: "S.md"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<h3>One-time</h3>`,
		`<a href="https://paypal.me/ExampleName/1USD" target="_blank" rel="noopener">$1</a>`,
		`<a href="https://paypal.me/ExampleName/2.5USD" target="_blank" rel="noopener">$2.50</a>`,
		`<h3>Monthly</h3>`,
		`<a href="https://www.paypal.com/webapps/billing/plans/subscribe?plan_id=P-5AB" target="_blank" rel="noopener">$5/mo</a>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("block lacks %q:\n%s", want, got)
		}
	}

	var warned string
	r = &Renderer{Warn: func(_ string, _ int, m string) { warned = m }}
	if got, _ := r.supportBlock("", 1, Context{Path: "S.md"}); got != "" || !strings.Contains(warned, "site.support") {
		t.Errorf("without config: %q, warning %q", got, warned)
	}
}

func TestSupportBlockMonthlyForm(t *testing.T) {
	r := &Renderer{Warn: func(string, int, string) {}}
	r.Support = &config.Support{PayPalMe: "ExampleName", Currency: "USD", Amounts: []float64{2},
		Monthly:        []config.MonthlyPlan{{Amount: 2, PlanID: "P-2AB"}, {Amount: 5, PlanID: "P-5AB"}},
		PayPalClientID: "AbC-123_xyzAbC-123_xyz"}
	got, err := r.supportBlock("", 1, Context{Path: "S.md"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<a href="https://paypal.me/ExampleName/2USD"`, // one-time unchanged
		`<div class="support-monthly" data-paypal-client="AbC-123_xyzAbC-123_xyz">`,
		`<input id="support-towns" class="support-towns" type="text" maxlength="127"`,
		`<label class="support-towns-label" for="support-towns">`,
		// the links still subscribe without the script
		`<a href="https://www.paypal.com/webapps/billing/plans/subscribe?plan_id=P-2AB" target="_blank" rel="noopener" data-plan="P-2AB">$2/mo</a>`,
		`data-plan="P-5AB">$5/mo</a>`,
		`<div class="support-paypal" hidden></div>`,
		`<p class="support-status" role="status" hidden></p>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("block lacks %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "<h3>Monthly</h3>") != 1 {
		t.Errorf("one Monthly heading expected:\n%s", got)
	}

	// Without a client ID, the monthly plans are plain links, as before.
	r.Support.PayPalClientID = ""
	got, _ = r.supportBlock("", 1, Context{Path: "S.md"})
	if strings.Contains(got, "support-towns") || strings.Contains(got, "data-plan") {
		t.Errorf("towns form without a client ID:\n%s", got)
	}
}

func TestMoney(t *testing.T) {
	for in, want := range map[float64]string{1: "$1", 2.5: "$2.50", 10: "$10"} {
		if got := money(in, "USD"); got != want {
			t.Errorf("money(%v) = %q, want %q", in, got, want)
		}
	}
	if got := money(5, "EUR"); got != "5 EUR" {
		t.Errorf("EUR: %q", got)
	}
}
