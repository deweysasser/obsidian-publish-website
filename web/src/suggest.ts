// The suggestion form at /suggest/ (site.suggest). A classic script,
// compiled to internal/target/html/assets/suggest.js and loaded only on that
// page.
//
// The kinds of suggestion and their fields come from the site's config; the
// generator renders each kind as a .suggest-kind[data-kind] block. ?kind=<id>
// picks one (the first if absent or unknown) and ?page=/a/b/ fills the page
// field. The Turnstile site key comes from GET {endpoint}, so rotating it
// needs no rebuild. The form posts its FormData to the same endpoint, which
// answers {"ok": true} or {"ok": false, "error": "..."}.

interface Turnstile {
  render(el: HTMLElement, options: { sitekey: string }): string;
  reset(widget: string): void;
}

interface Window {
  turnstile?: Turnstile;
}

(function () {
  const form = document.querySelector<HTMLFormElement>("form.suggest-form");
  const status = form?.querySelector<HTMLElement>(".suggest-status");
  const send = form?.querySelector<HTMLButtonElement>("button[type=submit]");
  const box = form?.querySelector<HTMLElement>(".suggest-turnstile");
  const pageInput = form?.querySelector<HTMLInputElement>("input[name=page]");
  const pageRow = form?.querySelector<HTMLElement>(".suggest-page");
  if (!form || !status || !send || !box || !pageInput || !pageRow) return;
  const endpoint = form.dataset.endpoint || "";
  let widget: string | null = null;

  const say = function (msg: string, error = false): void {
    status.textContent = msg;
    status.hidden = !msg;
    status.classList.toggle("error", error);
  };

  // A page path the form will send: a path on this site, nothing odd.
  const validPage = function (p: string | null): p is string {
    return !!p && p.length <= 300 && /^\/(?!\/)[^\s<>"`]*$/.test(p);
  };

  const kindBlocks = Array.from(form.querySelectorAll<HTMLElement>(".suggest-kind"));

  // setKind shows that kind's block and disables the others' inputs, so they
  // are neither validated nor submitted. Page and contact are shared, so
  // they keep what the visitor typed.
  const setKind = function (id: string | null): void {
    const ids = kindBlocks.map(function (el) { return el.dataset.kind || ""; });
    const kind = id && ids.indexOf(id) >= 0 ? id : ids[0];
    const radio = form.querySelector<HTMLInputElement>('input[name=kind][type=radio][value="' + kind + '"]');
    if (radio) radio.checked = true;
    kindBlocks.forEach(function (el) {
      const on = el.dataset.kind === kind;
      el.hidden = !on;
      el.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>("input, textarea").forEach(function (f) { f.disabled = !on; });
    });
  };

  const q = new URLSearchParams(location.search);
  const page = q.get("page");
  if (validPage(page)) {
    pageInput.value = page;
    pageRow.hidden = false;
  }
  setKind(q.get("kind"));
  form.querySelectorAll<HTMLInputElement>("input[name=kind][type=radio]").forEach(function (r) {
    r.addEventListener("change", function () { setKind(r.value); });
  });

  // Turnstile, rendered with the site key the endpoint publishes.
  fetch(endpoint, { headers: { Accept: "application/json" } })
    .then(function (r) { return r.json(); })
    .then(function (d: { turnstile_site_key?: string }) {
      const key = d && d.turnstile_site_key;
      if (!key) throw new Error("no site key");
      const s = document.createElement("script");
      s.src = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";
      s.async = true;
      s.onload = function () {
        if (window.turnstile) widget = window.turnstile.render(box, { sitekey: key });
      };
      s.onerror = function () { say("The anti-spam check couldn't load. Please reload the page.", true); };
      document.head.appendChild(s);
    })
    .catch(function () {
      say("The suggestion form isn't available just now. Please try again later.", true);
      send.disabled = true;
    });

  const thanks = function (): void {
    const done = document.createElement("div");
    done.className = "suggest-done";
    done.setAttribute("role", "status");
    const h = document.createElement("h2");
    h.textContent = "Thank you!";
    const p = document.createElement("p");
    p.textContent = "Your suggestion has been sent.";
    done.appendChild(h);
    done.appendChild(p);
    form.replaceWith(done);
  };

  // answer shows what the endpoint said. res is null when the request
  // failed; body is null when the answer wasn't JSON. It returns true on
  // success.
  const answer = function (res: Response | null, body: unknown): boolean {
    if (!res) {
      say("The suggestion couldn't be sent. Check your connection and try again.", true);
      return false;
    }
    if (!body || typeof body !== "object") {
      // e.g. Cloudflare's rate limit, which answers 429 with an HTML page
      say("Too many submissions. Please wait a few seconds and try again.", true);
      return false;
    }
    const b = body as { ok?: unknown; error?: unknown };
    if (res.ok && b.ok === true) {
      thanks();
      return true;
    }
    say(typeof b.error === "string" && b.error ? b.error : "Something went wrong. Please try again.", true);
    return false;
  };

  form.addEventListener("submit", function (e: Event) {
    e.preventDefault();
    if (!form.reportValidity()) return;
    say("");
    send.disabled = true;
    let res: Response | null = null;
    fetch(endpoint, { method: "POST", body: new FormData(form) })
      .then(function (r) {
        res = r;
        return r.json().catch(function () { return null; });
      })
      .catch(function () { return null; })
      .then(function (body) {
        if (!answer(res, body)) {
          send.disabled = false;
          if (widget !== null && window.turnstile) window.turnstile.reset(widget);
        }
      });
  });
})();
