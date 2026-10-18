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
})();
