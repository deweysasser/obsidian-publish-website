// The html target's page script: navigation, responsive tables, YouTube
// players, and the monthly-support form. A classic script (no imports or
// exports), compiled to internal/target/html/assets/app.js and loaded on
// every page.

interface PayPalButtonsConfig {
  style?: { label?: string };
  createSubscription(data: unknown, actions: { subscription: { create(o: { plan_id: string; custom_id?: string }): Promise<string> } }): Promise<string>;
  onApprove(): void;
  onError(): void;
}

// What we use of PayPal's JS SDK. Buttons is optional: a script that loads
// but isn't the SDK (a block page, a proxy's error) won't have it.
interface PayPalSDK {
  Buttons?(config: PayPalButtonsConfig): { render(el: HTMLElement): void };
}

interface Window {
  paypal?: PayPalSDK;
}

(function () {
  const btn = document.querySelector<HTMLElement>(".nav-toggle");
  const nav = document.getElementById("site-nav");
  if (btn && nav) {
    btn.addEventListener("click", function () {
      const open = document.body.classList.toggle("nav-open");
      btn.setAttribute("aria-expanded", open ? "true" : "false");
    });
  }
  const cur = nav && nav.querySelector<HTMLElement>("a[aria-current=page]");
  if (cur && cur.scrollIntoView) cur.scrollIntoView({ block: "nearest" });

  // Wide tables become labelled cards on narrow screens (see .stack in CSS).
  document.querySelectorAll<HTMLTableElement>(".content table").forEach(function (t) {
    const heads = t.querySelectorAll("thead th");
    if (heads.length < 4) return;
    const labels = Array.from(heads, function (th) {
      return Array.from(th.childNodes)
        .filter(function (n) {
          return !(n instanceof Element && n.classList.contains("small-text"));
        })
        .map(function (n) { return n.textContent; })
        .join("")
        .trim();
    });
    t.classList.add("stack");
    t.querySelectorAll("tbody tr").forEach(function (r) {
      Array.from(r.children).forEach(function (c, i) {
        if (labels[i]) c.setAttribute("data-label", labels[i]);
      });
    });
  });

  // YouTube: thumbnails become players on click; transcript timestamps (and
  // any other link to a video on this page) seek the player instead of
  // leaving the page. A player scrolled out of view docks in the corner.
  interface VideoRef { id: string; t: number }

  function ytRef(href: string): VideoRef | null {
    let u: URL;
    try { u = new URL(href, location.href); } catch (e) { return null; }
    const host = u.hostname.replace(/^(www|m)\./, "");
    let id: string | null = null;
    if (host === "youtu.be") id = u.pathname.slice(1).split("/")[0];
    else if (host === "youtube.com" || host === "youtube-nocookie.com") {
      id = u.searchParams.get("v");
      const m = !id && u.pathname.match(/^\/(?:live|shorts|embed)\/([^/]+)/);
      if (m) id = m[1];
    }
    if (!id || !/^[\w-]{11}$/.test(id)) return null;
    const t = u.searchParams.get("t") || u.searchParams.get("start") || "0";
    let secs = 0;
    const parts = t.match(/^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s?)?$/);
    if (parts) secs = (+parts[1] || 0) * 3600 + (+parts[2] || 0) * 60 + (+parts[3] || 0);
    return { id: id, t: secs };
  }
  function command(frame: HTMLIFrameElement, func: string, args?: unknown[]): void {
    frame.contentWindow?.postMessage(JSON.stringify({ event: "command", func: func, args: args || [] }), "*");
  }
  function play(box: HTMLElement, t: number): void {
    const existing = box.querySelector("iframe");
    if (existing) {
      command(existing, "seekTo", [t, true]);
      command(existing, "playVideo");
      return;
    }
    const wrap = document.createElement("div");
    wrap.className = "yt-frame";
    const close = document.createElement("button");
    close.className = "yt-close";
    close.setAttribute("aria-label", "Close player");
    close.textContent = "×";
    close.addEventListener("click", function () {
      const f = wrap.querySelector("iframe");
      if (f) command(f, "pauseVideo");
      wrap.classList.remove("docked");
      box.dataset.undock = "1";
    });
    const frame = document.createElement("iframe");
    frame.src = "https://www.youtube-nocookie.com/embed/" + box.dataset.yt + "?autoplay=1&enablejsapi=1&rel=0&start=" + t;
    frame.allow = "autoplay; encrypted-media; picture-in-picture; fullscreen";
    frame.allowFullscreen = true;
    frame.title = "Video";
    frame.dataset.t = String(t);
    frame.addEventListener("load", function () {
      // subscribe to player events so a refused embed can fall back to a link
      frame.contentWindow?.postMessage(JSON.stringify({ event: "listening", id: box.dataset.yt, channel: "widget" }), "*");
    });
    wrap.appendChild(close);
    wrap.appendChild(frame);
    box.textContent = "";
    box.appendChild(wrap);
    if ("IntersectionObserver" in window) {
      new IntersectionObserver(function (es) {
        const out = !es[0].isIntersecting;
        if (!out) delete box.dataset.undock;
        wrap.classList.toggle("docked", out && !box.dataset.undock);
      }).observe(box);
    }
  }
  function boxFor(id: string, near: Element): HTMLElement {
    const found = document.querySelector<HTMLElement>('.yt[data-yt="' + id + '"]');
    if (found) return found;
    const box = document.createElement("span");
    box.className = "yt";
    box.dataset.yt = id;
    near.parentNode?.insertBefore(box, near);
    return box;
  }
  window.addEventListener("message", function (e: MessageEvent) {
    if (!/^https:\/\/www\.youtube(-nocookie)?\.com$/.test(e.origin)) return;
    let d: { event?: string; info?: number } | null;
    try { d = JSON.parse(e.data); } catch (err) { return; }
    if (!d || d.event !== "onError" || [100, 101, 150].indexOf(d.info as number) < 0) return;
    document.querySelectorAll<HTMLIFrameElement>(".yt iframe").forEach(function (f) {
      if (f.contentWindow !== e.source) return;
      const box = f.closest<HTMLElement>(".yt");
      if (!box) return;
      const id = box.dataset.yt;
      const a = document.createElement("a");
      a.href = "https://youtu.be/" + id + "?t=" + (f.dataset.t || 0);
      a.className = "yt-refused";
      a.textContent = "This video can't be played here. Watch it on YouTube ↗";
      box.textContent = "";
      box.appendChild(a);
      box.classList.add("noembed");
      document.querySelectorAll("details.transcript").forEach(function (tr) { tr.setAttribute("data-noembed", ""); });
    });
  });
  document.addEventListener("click", function (e: MouseEvent) {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    const a = e.target instanceof Element ? e.target.closest<HTMLAnchorElement>("a[href]") : null;
    if (!a) return;
    const parent = a.parentNode;
    const thumb = parent instanceof HTMLElement && parent.classList.contains("yt") ? parent : null;
    const ref = ytRef(a.href);
    if (!ref) return;
    if (thumb && thumb.classList.contains("noembed")) return;
    const tr = a.closest("details.transcript");
    if (tr && tr.hasAttribute("data-noembed")) return;
    if (document.querySelector('.yt.noembed[data-yt="' + ref.id + '"]')) return;
    if (!thumb && !tr && !document.querySelector('.yt[data-yt="' + ref.id + '"]')) return;
    e.preventDefault();
    const box = thumb || boxFor(ref.id, tr || a);
    play(box, ref.t);
    const line = a.closest(".transcript-lines p");
    if (line) {
      const prev = document.querySelector(".transcript-lines p.playing");
      if (prev) prev.classList.remove("playing");
      line.classList.add("playing");
    }
  });

  // Monthly support (a ```support block with site.support.paypal_client_id):
  // the amount links choose a plan, and PayPal's subscribe button stores the
  // free-text towns on the subscription as its custom_id. PayPal's script is
  // loaded only once someone picks an amount; if it can't load, the links go
  // back to being plain subscribe links (without the towns).
  const sm = document.querySelector<HTMLElement>(".support-monthly[data-paypal-client]");
  const towns = sm?.querySelector<HTMLInputElement>(".support-towns");
  const slot = sm?.querySelector<HTMLElement>(".support-paypal");
  const status = sm?.querySelector<HTMLElement>(".support-status");
  if (sm && towns && slot && status) {
    let plan = "";
    let sdk: Promise<PayPalSDK> | null = null;
    const say = function (msg: string): void { status.textContent = msg; status.hidden = !msg; };
    // PayPal allows 127 characters in custom_id. Keep printable ASCII,
    // folding accented letters ("Española" becomes "Espanola").
    const customID = function (): string {
      return (towns.value || "").normalize("NFD").replace(/[̀-ͯ]/g, "")
        .replace(/[^\x20-\x7e]/g, "").replace(/\s+/g, " ").trim().slice(0, 127);
    };
    const loadSDK = function (): Promise<PayPalSDK> {
      if (!sdk) {
        sdk = new Promise(function (ok, fail) {
          const s = document.createElement("script");
          s.src = "https://www.paypal.com/sdk/js?client-id=" + encodeURIComponent(sm.dataset.paypalClient || "") +
            "&vault=true&intent=subscription&components=buttons";
          s.onload = function () {
            const p = window.paypal;
            if (p && typeof p.Buttons === "function") ok(p); else fail(new Error("PayPal SDK missing"));
          };
          s.onerror = function () { fail(new Error("PayPal SDK failed to load")); };
          document.head.appendChild(s);
        });
      }
      return sdk;
    };
    sm.addEventListener("click", function (e: MouseEvent) {
      const a = e.target instanceof Element ? e.target.closest<HTMLAnchorElement>("a[data-plan]") : null;
      if (!a || sm.dataset.fallback) return;
      e.preventDefault();
      plan = a.dataset.plan || "";
      sm.querySelectorAll("a[data-plan]").forEach(function (x) {
        x.classList.toggle("selected", x === a);
        if (x === a) x.setAttribute("aria-current", "true"); else x.removeAttribute("aria-current");
      });
      say("");
      loadSDK().then(function (paypal) {
        slot.hidden = false;
        if (slot.dataset.ready) return; // one button; it reads the plan chosen last
        slot.dataset.ready = "1";
        paypal.Buttons!({
          style: { label: "subscribe" },
          createSubscription: function (_data, actions) {
            const o: { plan_id: string; custom_id?: string } = { plan_id: plan };
            const c = customID();
            if (c) o.custom_id = c;
            return actions.subscription.create(o);
          },
          onApprove: function () {
            slot.hidden = true;
            say("Thank you! Your monthly support is set up. You can cancel it at any time from your PayPal account.");
          },
          onError: function () {
            say("PayPal couldn't start the subscription. Please try again, or use the amount link.");
          },
        }).render(slot);
      }, function () {
        sm.dataset.fallback = "1";
        say("PayPal's button couldn't load. Choose an amount again to subscribe on PayPal's site (the towns won't be included).");
      });
    });
  }
})();
