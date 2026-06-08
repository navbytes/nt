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
    // The /graph page needs "loose" so its server-generated click links work;
    // note-embedded diagrams stay "strict" (untrusted-ish note content).
    var graph = !!document.querySelector(".graphview");
    mermaid.initialize({ startOnLoad: false, securityLevel: graph ? "loose" : "strict", theme: isDark() ? "dark" : "neutral" });
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

  // ---- Reading width (cozy → wide → full), persisted like the theme ----
  var WIDTHS = ["cozy", "wide", "full"];
  var wtoggle = document.getElementById("width-toggle");
  if (wtoggle) wtoggle.addEventListener("click", function () {
    var cur = root.getAttribute("data-width") || "cozy";
    var next = WIDTHS[(WIDTHS.indexOf(cur) + 1) % WIDTHS.length];
    if (next === "cozy") root.removeAttribute("data-width");
    else root.setAttribute("data-width", next);
    localStorage.setItem("nt-width", next);
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

  // ---- Live updates via server-sent events (/events) ----
  // Typed payloads: "reload" = page is stale (external change), reload it; a
  // finer kind (e.g. "tasks") is dispatched as a "nt:<kind>" DOM event so a
  // listener can refresh just one fragment instead of the whole page.
  window.onReload = function () { location.reload(); };
  try {
    var es = new EventSource("/events");
    es.onmessage = function (e) {
      var kind = (e.data || "reload").trim();
      if (kind === "reload") { window.onReload(); return; }
      document.dispatchEvent(new CustomEvent("nt:" + kind));
    };
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

  // ---- Persist folder collapse state ----
  document.querySelectorAll("details.folder[data-path]").forEach(function (dt) {
    var key = "nt-folder:" + dt.getAttribute("data-path");
    if (localStorage.getItem(key) === "closed") dt.removeAttribute("open");
    dt.addEventListener("toggle", function () {
      localStorage.setItem(key, dt.open ? "open" : "closed");
    });
  });

  // ---- Recently viewed (record on note pages, render on the landing) ----
  (function () {
    var KEY = "nt-recent";
    function load() { try { return JSON.parse(localStorage.getItem(KEY)) || []; } catch (e) { return []; } }
    var title = document.querySelector(".note__title");
    if (title && location.pathname.indexOf("/n/") === 0) {
      var rec = load().filter(function (r) { return r.u !== location.pathname; });
      rec.unshift({ t: title.textContent, u: location.pathname });
      localStorage.setItem(KEY, JSON.stringify(rec.slice(0, 8)));
    }
    var box = document.getElementById("recent"), list = document.getElementById("recent-list");
    if (box && list) {
      var items = load();
      if (items.length) {
        list.innerHTML = items.map(function () { return "<li><a></a></li>"; }).join("");
        var as = list.querySelectorAll("a");
        items.forEach(function (r, i) { if (as[i]) { as[i].textContent = r.t; as[i].setAttribute("href", r.u); } });
        box.hidden = false;
      }
    }
  })();

  // ---- Command palette (⌘K / Ctrl+K) — fuzzy jump to a note ----
  (function () {
    var notes = [];
    try { notes = JSON.parse(document.getElementById("nt-notes").textContent) || []; } catch (e) { /* */ }
    var pal = document.getElementById("palette"),
        inp = document.getElementById("palette-input"),
        list = document.getElementById("palette-list");
    if (!pal) return;
    var sel = 0, shown = [];
    function render(q) {
      q = q.toLowerCase();
      shown = notes.filter(function (n) { return !q || (n.Title + " " + n.Path).toLowerCase().indexOf(q) >= 0; }).slice(0, 30);
      sel = 0;
      list.innerHTML = shown.map(function (n, i) {
        return '<li data-url="' + n.URL + '"' + (i === 0 ? ' class="sel"' : "") + '><span class="t"></span><span class="results__path"></span></li>';
      }).join("");
      var lis = list.querySelectorAll("li");
      shown.forEach(function (n, i) { lis[i].querySelector(".t").textContent = n.Title; lis[i].querySelector(".results__path").textContent = n.Path; });
    }
    function markSel() {
      var lis = list.querySelectorAll("li");
      lis.forEach(function (li, i) { li.classList.toggle("sel", i === sel); });
      if (lis[sel]) lis[sel].scrollIntoView({ block: "nearest" });
    }
    function open() { pal.hidden = false; inp.value = ""; render(""); inp.focus(); }
    function close() { pal.hidden = true; }
    document.addEventListener("keydown", function (e) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") { e.preventDefault(); pal.hidden ? open() : close(); return; }
      if (pal.hidden) return;
      if (e.key === "Escape") close();
      else if (e.key === "ArrowDown") { sel = Math.min(sel + 1, shown.length - 1); markSel(); e.preventDefault(); }
      else if (e.key === "ArrowUp") { sel = Math.max(sel - 1, 0); markSel(); e.preventDefault(); }
      else if (e.key === "Enter" && shown[sel]) { location.href = shown[sel].URL; }
    });
    inp.addEventListener("input", function () { render(inp.value); });
    list.addEventListener("click", function (e) { var li = e.target.closest("li"); if (li) location.href = li.getAttribute("data-url"); });
    pal.addEventListener("click", function (e) { if (e.target === pal) close(); });
  })();

  // ---- Search-as-you-type (debounced; degrades to the form submit) ----
  (function () {
    var inp = document.getElementById("search"), dd = document.getElementById("search-dropdown");
    if (!inp || !dd) return;
    var timer, sel = -1, rows = [];
    function hide() { dd.hidden = true; sel = -1; }
    function run(q) {
      if (!q.trim()) { hide(); return; }
      fetch("/search?json=1&q=" + encodeURIComponent(q))
        .then(function (r) { return r.json(); })
        .then(function (data) {
          rows = (data || []).slice(0, 12); sel = -1;
          if (!rows.length) { hide(); return; }
          dd.innerHTML = rows.map(function (r) {
            return '<li><a href="' + r.URL + '"><span class="t"></span><span class="results__path"></span></a></li>';
          }).join("");
          var lis = dd.querySelectorAll("li");
          rows.forEach(function (r, i) { lis[i].querySelector(".t").textContent = r.Title; lis[i].querySelector(".results__path").textContent = r.Path; });
          dd.hidden = false;
        }).catch(hide);
    }
    inp.addEventListener("input", function () { clearTimeout(timer); timer = setTimeout(function () { run(inp.value); }, 140); });
    inp.addEventListener("keydown", function (e) {
      if (dd.hidden) return;
      var lis = dd.querySelectorAll("li");
      if (e.key === "ArrowDown") sel = Math.min(sel + 1, lis.length - 1);
      else if (e.key === "ArrowUp") sel = Math.max(sel - 1, 0);
      else if (e.key === "Enter" && sel >= 0) { e.preventDefault(); location.href = rows[sel].URL; return; }
      else if (e.key === "Escape") { hide(); return; }
      else return;
      e.preventDefault();
      lis.forEach(function (li, i) { li.classList.toggle("sel", i === sel); });
    });
    document.addEventListener("click", function (e) { if (!dd.contains(e.target) && e.target !== inp) hide(); });
  })();

  // ---- Hover link previews ----
  (function () {
    var SEL = ".md a.wikilink, .backlinks a, .pager a";
    var pop, timer, cache = {};
    function popover() {
      if (!pop) { pop = document.createElement("div"); pop.className = "linkpop"; pop.hidden = true; document.body.appendChild(pop); }
      return pop;
    }
    function place(a, d) {
      if (!d || (!d.title && !d.snippet)) return;
      var p = popover();
      p.innerHTML = '<div class="linkpop__title"></div><div class="linkpop__body"></div>';
      p.querySelector(".linkpop__title").textContent = d.title || "";
      p.querySelector(".linkpop__body").textContent = d.snippet || "";
      var r = a.getBoundingClientRect();
      p.style.left = Math.max(8, Math.min(r.left, window.innerWidth - 340)) + "px";
      p.style.top = (r.bottom + 6 + window.scrollY) + "px";
      p.hidden = false;
    }
    function show(a) {
      var href = a.getAttribute("href");
      if (!href || href.indexOf("/n/") !== 0 || href.indexOf("missing=1") >= 0) return;
      if (cache[href]) { place(a, cache[href]); return; }
      fetch(href + (href.indexOf("?") >= 0 ? "&" : "?") + "preview=1")
        .then(function (r) { return r.json(); })
        .then(function (d) { cache[href] = d; place(a, d); }).catch(function () { /* */ });
    }
    function hide() { if (pop) pop.hidden = true; }
    document.addEventListener("mouseover", function (e) {
      var a = e.target.closest && e.target.closest(SEL);
      if (!a) return;
      clearTimeout(timer); timer = setTimeout(function () { show(a); }, 350);
    });
    document.addEventListener("mouseout", function (e) {
      if (e.target.closest && e.target.closest(SEL)) { clearTimeout(timer); hide(); }
    });
  })();

  // ---- Editing (nt web --edit): raw-file textarea, CSRF-guarded save ----
  (function () {
    var btn = document.getElementById("edit-btn");
    var md = document.querySelector(".md");
    if (!btn || !md) return;
    var csrf = (document.querySelector('meta[name="csrf"]') || {}).content || "";
    var etag = "";
    function openEditor() {
      if (document.querySelector(".editor")) return;
      fetch(location.pathname + "?raw=1").then(function (r) { etag = r.headers.get("ETag") || ""; return r.text(); }).then(function (text) {
        var wrap = document.createElement("div");
        wrap.className = "editwrap";
        var ta = document.createElement("textarea");
        ta.className = "editor";
        ta.value = text;
        ta.spellcheck = false;
        var bar = document.createElement("div");
        bar.className = "editbar";
        var status = document.createElement("span");
        status.className = "editbar__status";
        var cancel = document.createElement("button");
        cancel.className = "btn btn--ghost";
        cancel.textContent = "Cancel";
        var save = document.createElement("button");
        save.className = "btn";
        save.textContent = "Save";
        bar.appendChild(status);
        bar.appendChild(cancel);
        bar.appendChild(save);
        wrap.appendChild(ta);
        wrap.appendChild(bar);
        md.style.display = "none";
        md.parentNode.insertBefore(wrap, md.nextSibling);
        ta.focus();
        function close() { wrap.remove(); md.style.display = ""; }
        function commit() {
          save.disabled = true;
          status.textContent = "Saving…";
          var headers = { "X-CSRF": csrf, "Content-Type": "text/plain" };
          if (etag) headers["If-Match"] = etag; // lost-update guard (409 if changed underneath)
          fetch(location.pathname, { method: "POST", headers: headers, body: ta.value })
            .then(function (r) {
              if (r.ok) { location.reload(); }
              else if (r.status === 409) { save.disabled = false; status.textContent = "Changed on disk — reload to merge"; }
              else { save.disabled = false; status.textContent = "Save failed (" + r.status + ")"; }
            }).catch(function () { save.disabled = false; status.textContent = "Save failed"; });
        }
        cancel.addEventListener("click", close);
        save.addEventListener("click", commit);
        ta.addEventListener("keydown", function (e) {
          if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "s") { e.preventDefault(); commit(); }
          else if (e.key === "Escape") { close(); }
        });
      });
    }
    btn.addEventListener("click", openEditor);
    document.addEventListener("keydown", function (e) {
      if (e.key === "e" && !/^(INPUT|TEXTAREA)$/.test(document.activeElement.tagName) && !document.querySelector(".editor")) {
        e.preventDefault(); openEditor();
      }
    });
  })();

  enhanceReading();
  revealCurrent();
  renderMermaid();
})();
