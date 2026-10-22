(function () {
  var btn = document.querySelector(".nav-toggle");
  var nav = document.getElementById("site-nav");
  if (btn && nav) {
    btn.addEventListener("click", function () {
      var open = document.body.classList.toggle("nav-open");
      btn.setAttribute("aria-expanded", open ? "true" : "false");
    });
  }
  var cur = nav && nav.querySelector("a[aria-current=page]");
  if (cur && cur.scrollIntoView) cur.scrollIntoView({ block: "nearest" });

  // YouTube: thumbnails become players on click; transcript timestamps (and
  // any other link to a video on this page) seek the player instead of
  // leaving the page. A player scrolled out of view docks in the corner.
  function ytRef(href) {
    var u;
    try { u = new URL(href, location.href); } catch (e) { return null; }
    var host = u.hostname.replace(/^(www|m)\./, ""), id = null;
    if (host === "youtu.be") id = u.pathname.slice(1).split("/")[0];
    else if (host === "youtube.com" || host === "youtube-nocookie.com") {
      id = u.searchParams.get("v");
      var m = !id && u.pathname.match(/^\/(?:live|shorts|embed)\/([^/]+)/);
      if (m) id = m[1];
    }
    if (!id || !/^[\w-]{11}$/.test(id)) return null;
    var t = u.searchParams.get("t") || u.searchParams.get("start") || "0", secs = 0;
    var parts = t.match(/^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s?)?$/);
    if (parts) secs = (+parts[1] || 0) * 3600 + (+parts[2] || 0) * 60 + (+parts[3] || 0);
    return { id: id, t: secs };
  }
  function command(frame, func, args) {
    frame.contentWindow.postMessage(JSON.stringify({ event: "command", func: func, args: args || [] }), "*");
  }
  function play(box, t) {
    var frame = box.querySelector("iframe");
    if (frame) {
      command(frame, "seekTo", [t, true]);
      command(frame, "playVideo");
      return;
    }
    var wrap = document.createElement("div");
    wrap.className = "yt-frame";
    var close = document.createElement("button");
    close.className = "yt-close";
    close.setAttribute("aria-label", "Close player");
    close.textContent = "×";
    close.addEventListener("click", function () {
      var f = wrap.querySelector("iframe");
      if (f) command(f, "pauseVideo");
      wrap.classList.remove("docked");
      box.dataset.undock = "1";
    });
    frame = document.createElement("iframe");
    frame.src = "https://www.youtube-nocookie.com/embed/" + box.dataset.yt + "?autoplay=1&enablejsapi=1&rel=0&start=" + t;
    frame.allow = "autoplay; encrypted-media; picture-in-picture; fullscreen";
    frame.allowFullscreen = true;
    frame.title = "Video";
    frame.dataset.t = t;
    frame.addEventListener("load", function () {
      // subscribe to player events so a refused embed can fall back to a link
      frame.contentWindow.postMessage(JSON.stringify({ event: "listening", id: box.dataset.yt, channel: "widget" }), "*");
    });
    wrap.appendChild(close);
    wrap.appendChild(frame);
    box.textContent = "";
    box.appendChild(wrap);
    if ("IntersectionObserver" in window) {
      new IntersectionObserver(function (es) {
        var out = !es[0].isIntersecting;
        if (!out) delete box.dataset.undock;
        wrap.classList.toggle("docked", out && !box.dataset.undock);
      }).observe(box);
    }
  }
  function boxFor(id, near) {
    var box = document.querySelector('.yt[data-yt="' + id + '"]');
    if (box) return box;
    box = document.createElement("span");
    box.className = "yt";
    box.dataset.yt = id;
    near.parentNode.insertBefore(box, near);
    return box;
  }
  window.addEventListener("message", function (e) {
    if (!/^https:\/\/www\.youtube(-nocookie)?\.com$/.test(e.origin)) return;
    var d;
    try { d = JSON.parse(e.data); } catch (err) { return; }
    if (!d || d.event !== "onError" || [100, 101, 150].indexOf(d.info) < 0) return;
    document.querySelectorAll(".yt iframe").forEach(function (f) {
      if (f.contentWindow !== e.source) return;
      var box = f.closest(".yt"), id = box.dataset.yt;
      var a = document.createElement("a");
      a.href = "https://youtu.be/" + id + "?t=" + (f.dataset.t || 0);
      a.className = "yt-refused";
      a.textContent = "This video can't be played here. Watch it on YouTube ↗";
      box.textContent = "";
      box.appendChild(a);
      box.classList.add("noembed");
      document.querySelectorAll("details.transcript").forEach(function (tr) { tr.setAttribute("data-noembed", ""); });
    });
  });
  document.addEventListener("click", function (e) {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    var a = e.target.closest && e.target.closest("a[href]");
    if (!a) return;
    var thumb = a.parentNode && a.parentNode.classList && a.parentNode.classList.contains("yt") ? a.parentNode : null;
    var ref = ytRef(a.href);
    if (!ref) return;
    if (thumb && thumb.classList.contains("noembed")) return;
    var tr = a.closest("details.transcript");
    if (tr && tr.hasAttribute("data-noembed")) return;
    if (document.querySelector('.yt.noembed[data-yt="' + ref.id + '"]')) return;
    if (!thumb && !tr && !document.querySelector('.yt[data-yt="' + ref.id + '"]')) return;
    e.preventDefault();
    var box = thumb || boxFor(ref.id, tr || a);
    play(box, ref.t);
    var line = a.closest(".transcript-lines p");
    if (line) {
      var prev = document.querySelector(".transcript-lines p.playing");
      if (prev) prev.classList.remove("playing");
      line.classList.add("playing");
    }
  });
})();
