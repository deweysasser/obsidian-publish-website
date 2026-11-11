// Tests for the compiled assets/app.js, run in happy-dom by Node's test
// runner: node --test test/*.test.ts (in a container; see the Makefile).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { Window } from "happy-dom";
import type { Element as El, HTMLInputElement as Input, HTMLElement as HElement, HTMLScriptElement as Script } from "happy-dom";

const appJS = readFileSync(new URL("../../internal/target/html/assets/app.js", import.meta.url), "utf8");

// page loads body into a fresh window and runs app.js on it. Scripts the
// page adds (PayPal's SDK) are never fetched: they fire "error", or "load"
// when scriptsLoad is set.
function page(body: string, scriptsLoad = false): Window {
  const window = new Window({
    url: "https://example.org/p/",
    settings: {
      enableJavaScriptEvaluation: true,
      disableJavaScriptFileLoading: true,
      disableCSSFileLoading: true,
      handleDisabledFileLoadingAsSuccess: scriptsLoad,
    },
  });
  window.document.body.innerHTML = body;
  const s = window.document.createElement("script");
  s.textContent = appJS;
  window.document.body.appendChild(s);
  return window;
}

function click(window: Window, el: El): boolean {
  const e = new window.MouseEvent("click", { bubbles: true, cancelable: true, button: 0 });
  return el.dispatchEvent(e); // false when the script called preventDefault
}

test("the nav toggle opens and closes the nav", () => {
  const w = page(`<button class="nav-toggle" aria-expanded="false"></button><nav id="site-nav"></nav>`);
  const btn = w.document.querySelector(".nav-toggle")!;
  click(w, btn);
  assert.equal(btn.getAttribute("aria-expanded"), "true");
  assert.ok(w.document.body.classList.contains("nav-open"));
  click(w, btn);
  assert.equal(btn.getAttribute("aria-expanded"), "false");
});

test("wide tables are labelled for narrow screens; narrow ones are left alone", () => {
  const w = page(`<div class="content">
    <table id="wide"><thead><tr><th>A</th><th>B <span class="small-text">(x)</span></th><th>C</th><th>D</th></tr></thead>
      <tbody><tr><td>1</td><td>2</td><td>3</td><td>4</td></tr></tbody></table>
    <table id="narrow"><thead><tr><th>A</th><th>B</th></tr></thead><tbody><tr><td>1</td><td>2</td></tr></tbody></table>
  </div>`);
  const wide = w.document.getElementById("wide")!;
  assert.ok(wide.classList.contains("stack"));
  const cells = wide.querySelectorAll("tbody td");
  assert.equal(cells[0].getAttribute("data-label"), "A");
  assert.equal(cells[1].getAttribute("data-label"), "B", "the small-text note is left out of the label");
  assert.ok(!w.document.getElementById("narrow")!.classList.contains("stack"));
});

test("a video thumbnail becomes a player at the link's timestamp", () => {
  const w = page(`<span class="yt" data-yt="abcdefghijk"><a href="https://www.youtube.com/watch?v=abcdefghijk&t=1h2m3s">thumb</a></span>`);
  const notPrevented = click(w, w.document.querySelector(".yt a")!);
  assert.equal(notPrevented, false, "the click stays on the page");
  const frame = w.document.querySelector(".yt iframe")!;
  assert.equal(frame.getAttribute("src"), "https://www.youtube-nocookie.com/embed/abcdefghijk?autoplay=1&enablejsapi=1&rel=0&start=3723");
});

test("ordinary links and videos not on the page are left alone", () => {
  const w = page(`<p><a id="ext" href="https://example.com/">x</a><a id="vid" href="https://youtu.be/zzzzzzzzzzz">y</a></p>`);
  assert.equal(click(w, w.document.getElementById("ext")!), true);
  assert.equal(click(w, w.document.getElementById("vid")!), true, "no player for it on this page, so the link opens");
});

// The monthly-support form, with a stub of PayPal's SDK.
const supportForm = `<div class="support-monthly" data-paypal-client="AbC-123_x">
  <input class="support-towns" value="">
  <ul class="support-amounts">
    <li><a id="p2" href="https://paypal.example/2" data-plan="P-2">$2/mo</a></li>
    <li><a id="p5" href="https://paypal.example/5" data-plan="P-5">$5/mo</a></li>
  </ul>
  <div class="support-paypal" hidden></div><p class="support-status" hidden></p>
</div>`;

interface Captured {
  createSubscription(data: unknown, actions: unknown): unknown;
  onApprove(): void;
}

function sdkScript(w: Window): Script {
  const s = w.document.head.querySelector('script[src^="https://www.paypal.com/sdk/js"]');
  assert.ok(s, "the SDK script was added");
  return s as unknown as Script;
}

test("monthly support: picking an amount loads the SDK and subscribes with the towns as custom_id", async () => {
  const w = page(supportForm, true);
  let captured: Captured | null = null;
  (w as unknown as { paypal: unknown }).paypal = {
    Buttons(config: Captured) {
      captured = config;
      return { render() {} };
    },
  };
  const towns = w.document.querySelector(".support-towns") as unknown as Input;
  towns.value = "  Española   Town, Worcester MA  ";
  assert.equal(click(w, w.document.getElementById("p5")!), false);
  assert.ok(w.document.getElementById("p5")!.classList.contains("selected"));
  assert.match(sdkScript(w).src, /client-id=AbC-123_x&vault=true&intent=subscription/);
  await new Promise((r) => setTimeout(r, 20));
  assert.ok(captured, "the subscribe button was rendered");
  assert.equal((w.document.querySelector(".support-paypal") as unknown as HElement).hidden, false);

  // The object comes from the page's script realm; compare it as plain data.
  const create = (o: unknown) => JSON.parse(JSON.stringify(o));
  const c = captured as Captured;
  assert.deepEqual(c.createSubscription(null, { subscription: { create } }), { plan_id: "P-5", custom_id: "Espanola Town, Worcester MA" });

  // the button uses the plan chosen last
  click(w, w.document.getElementById("p2")!);
  await new Promise((r) => setTimeout(r, 0));
  towns.value = "x".repeat(200);
  assert.deepEqual(c.createSubscription(null, { subscription: { create } }), { plan_id: "P-2", custom_id: "x".repeat(127) });
  towns.value = "   ";
  assert.deepEqual(c.createSubscription(null, { subscription: { create } }), { plan_id: "P-2" }, "no custom_id when empty");

  c.onApprove();
  assert.match(w.document.querySelector(".support-status")!.textContent!, /Thank you/);
});

test("monthly support: if the SDK can't load, the links become plain links again", async () => {
  const w = page(supportForm);
  click(w, w.document.getElementById("p5")!);
  sdkScript(w);
  await new Promise((r) => setTimeout(r, 20));
  assert.match(w.document.querySelector(".support-status")!.textContent!, /couldn't load/);
  assert.equal(click(w, w.document.getElementById("p5")!), true, "the next click follows the link");
});
