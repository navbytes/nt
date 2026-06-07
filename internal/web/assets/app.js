/* nt web — zero deps. mermaid + theme toggle + mobile nav + live reload. */
(function () {
  "use strict";

  var root = document.documentElement;

  // ---- Theme ↔ mermaid ----
  function isDark() {
    return root.getAttribute("data-theme") === "dark" ||
      (!root.hasAttribute("data-theme") &&
        window.matchMedia("(prefers-color-scheme: dark)").matches);
  }

  function renderMermaid() {
    if (!window.mermaid) return;
    mermaid.initialize({ startOnLoad: false, securityLevel: "strict", theme: isDark() ? "dark" : "neutral" });
    var nodes = document.querySelectorAll(".mermaid");
    nodes.forEach(function (n) {
      if (!n.dataset.src) n.dataset.src = n.textContent;   // stash source once
      n.removeAttribute("data-processed");
      n.innerHTML = n.dataset.src;
    });
    try { mermaid.run({ nodes: nodes }); } catch (e) { console.warn(e); }
  }

  var saved = localStorage.getItem("nt-theme");
  if (saved) root.setAttribute("data-theme", saved);

  var toggle = document.getElementById("theme-toggle");
  if (toggle) toggle.addEventListener("click", function () {
    var next = isDark() ? "light" : "dark";
    root.setAttribute("data-theme", next);
    localStorage.setItem("nt-theme", next);
    renderMermaid();
  });

  // ---- Sidebar collapse (mobile) ----
  var app = document.getElementById("app");
  document.querySelectorAll("[data-nav-toggle]").forEach(function (b) {
    b.addEventListener("click", function () { app.classList.toggle("nav-open"); });
  });
  document.querySelectorAll("[data-nav-close]").forEach(function (b) {
    b.addEventListener("click", function () { app.classList.remove("nav-open"); });
  });
  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape") app.classList.remove("nav-open");
    if (e.key === "/" && document.activeElement.tagName !== "INPUT") {
      e.preventDefault(); var s = document.getElementById("search"); if (s) s.focus();
    }
  });

  // ---- Live reload via server-sent events (/events) ----
  window.onReload = function () { location.reload(); };
  try {
    var es = new EventSource("/events");
    es.onmessage = function () { window.onReload(); };
  } catch (e) { /* SSE unavailable — static view still works */ }

  // ---- Reading enhancements: heading anchors, TOC, copy buttons ----
  function enhanceReading() {
    var md = document.querySelector(".md");
    if (!md) return;
    var heads = md.querySelectorAll("h2[id], h3[id]");

    // Build TOC (right rail) before mutating headings, if there's enough.
    if (heads.length >= 2) {
      var nav = document.createElement("nav");
      nav.className = "toc";
      nav.setAttribute("aria-label", "On this page");
      var html = '<div class="toc__title">On this page</div>';
      heads.forEach(function (h) {
        html += '<a href="#' + h.id + '" class="toc--' + h.tagName.toLowerCase() + '">' +
          h.textContent + "</a>";
      });
      nav.innerHTML = html;
      document.querySelector(".main").appendChild(nav);

      // Scrollspy: highlight the heading currently in view.
      if ("IntersectionObserver" in window) {
        var byId = {};
        nav.querySelectorAll("a").forEach(function (a) { byId[a.getAttribute("href").slice(1)] = a; });
        var obs = new IntersectionObserver(function (entries) {
          entries.forEach(function (e) {
            if (!e.isIntersecting) return;
            nav.querySelectorAll("a").forEach(function (a) { a.classList.remove("toc--active"); });
            if (byId[e.target.id]) byId[e.target.id].classList.add("toc--active");
          });
        }, { rootMargin: "-80px 0px -70% 0px" });
        heads.forEach(function (h) { obs.observe(h); });
      }
    }

    // Hover "#" anchors on headings (after TOC text was captured).
    md.querySelectorAll("h1[id], h2[id], h3[id], h4[id]").forEach(function (h) {
      var a = document.createElement("a");
      a.className = "heading-anchor";
      a.href = "#" + h.id;
      a.textContent = "#";
      a.setAttribute("aria-hidden", "true");
      h.appendChild(a);
    });

    // Copy button on code blocks (mermaid is a <div>, so it's skipped).
    md.querySelectorAll("pre").forEach(function (pre) {
      var btn = document.createElement("button");
      btn.className = "copy-btn";
      btn.type = "button";
      btn.textContent = "Copy";
      btn.addEventListener("click", function () {
        var code = pre.querySelector("code");
        navigator.clipboard.writeText(code ? code.textContent : pre.textContent).then(function () {
          btn.textContent = "Copied";
          btn.classList.add("copied");
          setTimeout(function () { btn.textContent = "Copy"; btn.classList.remove("copied"); }, 1200);
        });
      });
      pre.appendChild(btn);
    });
  }

  // ---- Scroll the current note into view in the sidebar tree ----
  function revealCurrent() {
    var cur = document.querySelector(".tree__link.is-current");
    if (cur) cur.scrollIntoView({ block: "center" });
  }

  enhanceReading();
  revealCurrent();
  renderMermaid();
})();
