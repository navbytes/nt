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

  renderMermaid();
})();
