// Tests for the compiled assets/suggest.js, against the form markup the
// generator renders (fixtures/suggest-form.html, kept current by a Go test),
// with fetch and Turnstile stubbed. Run by Node's test runner in happy-dom.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { Window } from "happy-dom";
import type { HTMLElement as HElement, HTMLFormElement as Form, HTMLInputElement as Input } from "happy-dom";

const suggestJS = readFileSync(new URL("../../internal/target/html/assets/suggest.js", import.meta.url), "utf8");
const formHTML = readFileSync(new URL("./fixtures/suggest-form.html", import.meta.url), "utf8");

interface Answer { status: number; json?: unknown } // json undefined: not JSON
interface Sent { url: string; method: string; fields: Record<string, string[]> }

interface Page {
  w: Window;
  form: Form;
  sent: Sent[];
  turnstile: { key: string | null; resets: number };
  field(name: string): Input;
  shown(kind: string): boolean;
  status(): string;
  submit(): Promise<void>;
}

const tick = () => new Promise((r) => setTimeout(r, 20));

// open loads the form page at ?query and runs suggest.js. GET answers with
// the site key (or fails, with getFails); each POST takes the next answer
// (or fails with a network error when there's none).
async function open(query: string, answers: Answer[] = [], getFails = false): Promise<Page> {
  const w = new Window({
    url: "https://example.org/suggest/" + query,
    settings: {
      enableJavaScriptEvaluation: true,
      disableJavaScriptFileLoading: true,
      disableCSSFileLoading: true,
      handleDisabledFileLoadingAsSuccess: true, // the Turnstile script "loads"
    },
  });
  const sent: Sent[] = [];
  const turnstile = { key: null as string | null, resets: 0 };
  const win = w as unknown as Record<string, unknown>;
  win.fetch = async (url: string, init?: { method?: string; body?: { entries(): Iterable<[string, string]> } }) => {
    const method = (init && init.method) || "GET";
    const fields: Record<string, string[]> = {};
    if (init && init.body) for (const [k, v] of init.body.entries()) (fields[k] ||= []).push(String(v));
    sent.push({ url, method, fields });
    if (method === "GET") {
      if (getFails) throw new TypeError("network");
      return { ok: true, status: 200, json: async () => ({ turnstile_site_key: "site-key-1" }) };
    }
    const a = answers.shift();
    if (!a) throw new TypeError("network");
    return {
      ok: a.status >= 200 && a.status < 300,
      status: a.status,
      json: async () => { if (a.json === undefined) throw new SyntaxError("not JSON"); return a.json; },
    };
  };
  win.turnstile = {
    render: (_el: unknown, o: { sitekey: string }) => { turnstile.key = o.sitekey; return "widget-1"; },
    reset: () => { turnstile.resets++; },
  };
  w.document.body.innerHTML = formHTML;
  const s = w.document.createElement("script");
  s.textContent = suggestJS;
  w.document.body.appendChild(s);
  await tick();
  const form = w.document.querySelector("form.suggest-form") as unknown as Form;
  return {
    w, form, sent, turnstile,
    field: (name) => form.querySelector(`[name=${name}]:not([disabled])`) as unknown as Input,
    shown: (kind) => !(form.querySelector(`.suggest-kind[data-kind="${kind}"]`) as unknown as HElement).hidden,
    status: () => (w.document.querySelector(".suggest-status") as unknown as HElement | null)?.textContent || "",
    submit: async () => {
      form.dispatchEvent(new w.Event("submit", { bubbles: true, cancelable: true }));
      await tick();
    },
  };
}

test("the kind and page come from the query", async () => {
  const p = await open("?kind=town&page=%2Fus%2Fma%2Fx%2F");
  assert.ok(p.shown("town") && !p.shown("correction"));
  assert.ok((p.form.querySelector("input[name=kind][value=town]") as unknown as Input).checked);
  assert.equal((p.form.querySelector("[name=page]") as unknown as Input).value, "/us/ma/x/");
  assert.equal((p.form.querySelector(".suggest-page") as unknown as HElement).hidden, false);
});

test("the first kind when the query names none or an unknown one; a bad page is ignored", async () => {
  for (const q of ["", "?kind=nope&page=javascript:alert(1)", "?page=%2F%2Fevil.example%2F"]) {
    const p = await open(q);
    assert.ok(p.shown("correction") && !p.shown("town"), q);
    assert.equal((p.form.querySelector("[name=page]") as unknown as Input).value, "", q);
    assert.equal((p.form.querySelector(".suggest-page") as unknown as HElement).hidden, true, q);
  }
});

test("switching kinds keeps page and contact, and submits only that kind's fields", async () => {
  const p = await open("?kind=town&page=%2Fa%2F", [{ status: 200, json: { ok: true } }]);
  p.field("contact").value = "me@example.org";
  const radio = p.form.querySelector("input[name=kind][value=correction]") as unknown as Input;
  radio.checked = true;
  radio.dispatchEvent(new p.w.Event("change"));
  assert.ok(p.shown("correction") && !p.shown("town"));
  assert.equal(p.field("contact").value, "me@example.org");
  p.field("message").value = "The meeting date is wrong.";
  await p.submit();
  const post = p.sent.find((s) => s.method === "POST");
  assert.ok(post, "posted");
  assert.equal(post.url, "/api/suggest");
  assert.deepEqual(post.fields.kind, ["correction"]);
  assert.deepEqual(post.fields.page, ["/a/"]);
  assert.equal(post.fields.message[0], "The meeting date is wrong.");
  assert.deepEqual(post.fields.contact, ["me@example.org"]);
  assert.equal(post.fields.town, undefined, "the hidden kind's fields aren't sent");
  // Browsers leave disabled controls out of FormData. happy-dom (21.0.4)
  // still includes a disabled <textarea>, so check that the hidden kind's
  // controls are disabled rather than counting messages.
  const hidden = p.form.querySelectorAll('.suggest-kind[data-kind="town"] input, .suggest-kind[data-kind="town"] textarea');
  assert.ok(hidden.length === 3 && Array.from(hidden).every((f) => (f as unknown as Input).disabled));
});

test("Turnstile is rendered with the key the endpoint publishes", async () => {
  const p = await open("");
  assert.equal(p.sent[0].method, "GET");
  assert.equal(p.sent[0].url, "/api/suggest");
  assert.equal(p.turnstile.key, "site-key-1");
});

test("if the endpoint can't be reached, the form says so and can't be sent", async () => {
  const p = await open("", [], true);
  assert.match(p.status(), /isn't available/);
  assert.equal((p.form.querySelector("button[type=submit]") as unknown as Input).disabled, true);
});

test("required fields are checked before anything is sent", async () => {
  const p = await open("?kind=town", [{ status: 200, json: { ok: true } }]);
  await p.submit(); // town and state empty
  assert.equal(p.sent.filter((s) => s.method === "POST").length, 0);
});

test("success replaces the form with a thank-you", async () => {
  const p = await open("?kind=town", [{ status: 200, json: { ok: true } }]);
  p.field("town").value = "Springfield";
  p.field("state").value = "Illinois";
  await p.submit();
  assert.equal(p.w.document.querySelector("form.suggest-form"), null);
  assert.match(p.w.document.querySelector(".suggest-done")!.textContent!, /Thank you/);
});

test("an error from the endpoint is shown, the form kept, and Turnstile reset", async () => {
  const p = await open("", [{ status: 403, json: { ok: false, error: "The anti-spam check failed. Please reload the page and try again." } }]);
  p.field("message").value = "x";
  await p.submit();
  assert.equal(p.status(), "The anti-spam check failed. Please reload the page and try again.");
  assert.equal(p.field("message").value, "x", "what the visitor typed is kept");
  assert.equal(p.turnstile.resets, 1);
  assert.equal((p.form.querySelector("button[type=submit]") as unknown as Input).disabled, false);
});

test("a non-JSON answer (Cloudflare's 429 page) asks the visitor to wait", async () => {
  const p = await open("", [{ status: 429 }]);
  p.field("message").value = "x";
  await p.submit();
  assert.match(p.status(), /Too many submissions/);
});

test("a network failure asks the visitor to try again", async () => {
  const p = await open("", []);
  p.field("message").value = "x";
  await p.submit();
  assert.match(p.status(), /Check your connection/);
  assert.equal(p.turnstile.resets, 1);
});
