/* nt web — interactive force-directed link graph (canvas, zero deps).
   Replaces the old Mermaid graph: pan, zoom, drag, hover-highlight neighbors,
   click to open, filter, and color by folder/source. */
(function () {
  "use strict";
  var box = document.getElementById("graph"), canvas = document.getElementById("graph-canvas");
  if (!box || !canvas) return;
  var data;
  try { data = JSON.parse(document.getElementById("nt-graph").textContent); } catch (e) { return; }
  if (!data || !data.nodes || !data.nodes.length) return;

  var nodes = data.nodes.map(function (n, i) {
    var a = (i / data.nodes.length) * Math.PI * 2;
    return {
      id: n.id, title: n.title || "(untitled)", url: n.url, folder: n.folder || "", source: n.source || "",
      tags: n.tags || [], deg: n.deg || 0, x: Math.cos(a) * 180, y: Math.sin(a) * 180, vx: 0, vy: 0
    };
  });
  var links = data.links.map(function (l) { return { s: nodes[l.s], t: nodes[l.t] }; });

  // adjacency (for hover neighborhood highlight)
  var adj = nodes.map(function () { return new Set(); });
  data.links.forEach(function (l) { adj[l.s].add(nodes[l.t]); adj[l.t].add(nodes[l.s]); });

  var ctx = canvas.getContext("2d"), dpr = window.devicePixelRatio || 1, W = 0, H = 0;
  function css(v, fb) { var c = getComputedStyle(document.documentElement).getPropertyValue(v).trim(); return c || fb; }
  var palette = ["#7aa2f7", "#7dcfff", "#bb9af7", "#9ece6a", "#e0af68", "#f7768e", "#2ac3de", "#ff9e64", "#b4f9f8", "#c0caf5"];
  var fg, accent, linkCol, cat = {};
  function theme() { fg = css("--fg", "#333"); accent = css("--accent", "#5a52d6"); linkCol = css("--border", "#ccc"); }
  theme();

  var colorBy = "folder";
  function colorOf(n) {
    var key = (colorBy === "source" ? n.source : n.folder) || "·";
    if (!(key in cat)) cat[key] = palette[Object.keys(cat).length % palette.length];
    return cat[key];
  }

  var view = { x: 0, y: 0, k: 1 };
  function toScreen(p) { return { x: p.x * view.k + view.x + W / 2, y: p.y * view.k + view.y + H / 2 }; }

  function resize() {
    var r = box.getBoundingClientRect();
    W = r.width; H = Math.max(360, window.innerHeight - 220);
    canvas.style.height = H + "px";
    canvas.width = W * dpr; canvas.height = H * dpr;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    draw();
  }

  // ---- simulation ----
  var alpha = 1, running = false, dragNode = null;
  function tick() {
    var i, j, a, b, dx, dy, d, d2, f;
    for (i = 0; i < nodes.length; i++) {
      a = nodes[i];
      for (j = i + 1; j < nodes.length; j++) {
        b = nodes[j];
        dx = a.x - b.x; dy = a.y - b.y; d2 = dx * dx + dy * dy + 0.01; d = Math.sqrt(d2);
        f = 900 / d2; dx = dx / d * f; dy = dy / d * f;
        a.vx += dx; a.vy += dy; b.vx -= dx; b.vy -= dy;
      }
    }
    links.forEach(function (l) {
      var dx = l.t.x - l.s.x, dy = l.t.y - l.s.y, d = Math.sqrt(dx * dx + dy * dy) + 0.01, f = (d - 70) * 0.02;
      dx = dx / d * f; dy = dy / d * f;
      l.s.vx += dx; l.s.vy += dy; l.t.vx -= dx; l.t.vy -= dy;
    });
    nodes.forEach(function (n) {
      n.vx += -n.x * 0.0025; n.vy += -n.y * 0.0025;
      if (n !== dragNode) { n.x += n.vx * alpha; n.y += n.vy * alpha; }
      n.vx *= 0.85; n.vy *= 0.85;
    });
    alpha *= 0.99;
  }
  function loop() {
    if (alpha > 0.02) tick();
    draw();
    if (alpha > 0.02 || dragNode) requestAnimationFrame(loop); else running = false;
  }
  function run() { if (!running) { running = true; requestAnimationFrame(loop); } }
  function reheat() { alpha = Math.max(alpha, 0.4); run(); }

  // ---- filter / hover state ----
  var hover = null, q = "";
  function match(n) {
    if (!q) return true;
    return (n.title + " " + n.folder + " " + n.source + " " + n.tags.join(" ")).toLowerCase().indexOf(q) >= 0;
  }

  function draw() {
    ctx.clearRect(0, 0, W, H);
    links.forEach(function (l) {
      var hot = hover && (l.s === hover || l.t === hover);
      var dim = (hover && !hot) || (q && !(match(l.s) && match(l.t)));
      ctx.strokeStyle = hot ? accent : linkCol;
      ctx.globalAlpha = dim ? 0.05 : (hot ? 0.8 : 0.3);
      ctx.lineWidth = hot ? 1.5 : 1;
      var a = toScreen(l.s), b = toScreen(l.t);
      ctx.beginPath(); ctx.moveTo(a.x, a.y); ctx.lineTo(b.x, b.y); ctx.stroke();
    });
    ctx.globalAlpha = 1;
    nodes.forEach(function (n) {
      var p = toScreen(n), r = (4 + Math.min(n.deg, 12) * 1.1) * Math.max(0.6, Math.sqrt(view.k));
      var hot = hover && (n === hover || adj[nodes.indexOf(n)].has(hover));
      var dim = (hover && !hot) || (q && !match(n));
      ctx.globalAlpha = dim ? 0.12 : 1;
      ctx.beginPath(); ctx.arc(p.x, p.y, r, 0, Math.PI * 2);
      ctx.fillStyle = colorOf(n); ctx.fill();
      if (n === hover) { ctx.lineWidth = 2; ctx.strokeStyle = fg; ctx.stroke(); }
      if (view.k > 1.25 || hot) {
        ctx.globalAlpha = dim ? 0.2 : 0.92; ctx.fillStyle = fg; ctx.font = "11px system-ui, sans-serif";
        ctx.fillText(n.title.length > 28 ? n.title.slice(0, 28) + "…" : n.title, p.x + r + 4, p.y + 4);
      }
    });
    ctx.globalAlpha = 1;
  }

  function nodeAt(sx, sy) {
    for (var i = nodes.length - 1; i >= 0; i--) {
      var p = toScreen(nodes[i]), r = (4 + Math.min(nodes[i].deg, 12) * 1.1) * Math.max(0.6, Math.sqrt(view.k)) + 3;
      if ((sx - p.x) * (sx - p.x) + (sy - p.y) * (sy - p.y) <= r * r) return nodes[i];
    }
    return null;
  }

  // ---- interaction ----
  var down = null, moved = false, panStart = null;
  function rel(e) { var r = canvas.getBoundingClientRect(); return { x: e.clientX - r.left, y: e.clientY - r.top }; }

  canvas.addEventListener("mousedown", function (e) {
    var m = rel(e); down = nodeAt(m.x, m.y); moved = false;
    panStart = { x: m.x, y: m.y, vx: view.x, vy: view.y };
    if (down) dragNode = down;
  });
  window.addEventListener("mousemove", function (e) {
    var m = rel(e);
    if (down !== undefined && panStart && (e.buttons & 1)) {
      moved = moved || Math.abs(m.x - panStart.x) + Math.abs(m.y - panStart.y) > 3;
      if (dragNode) { dragNode.x = (m.x - view.x - W / 2) / view.k; dragNode.y = (m.y - view.y - H / 2) / view.k; reheat(); }
      else { view.x = panStart.vx + (m.x - panStart.x); view.y = panStart.vy + (m.y - panStart.y); draw(); }
      return;
    }
    var h = nodeAt(m.x, m.y);
    canvas.style.cursor = h ? "pointer" : "grab";
    if (h !== hover) { hover = h; if (!running) draw(); }
  });
  window.addEventListener("mouseup", function (e) {
    if (down && !moved) { location.href = down.url; } // click a node → open it
    down = undefined; dragNode = null; panStart = null;
  });
  canvas.addEventListener("wheel", function (e) {
    e.preventDefault();
    var m = rel(e), wx = (m.x - view.x - W / 2) / view.k, wy = (m.y - view.y - H / 2) / view.k;
    view.k = Math.max(0.2, Math.min(5, view.k * (e.deltaY < 0 ? 1.1 : 0.9)));
    view.x = m.x - W / 2 - wx * view.k; view.y = m.y - H / 2 - wy * view.k;
    draw();
  }, { passive: false });

  function fit() {
    var minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    nodes.forEach(function (n) { minX = Math.min(minX, n.x); minY = Math.min(minY, n.y); maxX = Math.max(maxX, n.x); maxY = Math.max(maxY, n.y); });
    var w = maxX - minX || 1, h = maxY - minY || 1;
    view.k = Math.max(0.2, Math.min(2, 0.85 * Math.min(W / w, H / h)));
    view.x = -((minX + maxX) / 2) * view.k; view.y = -((minY + maxY) / 2) * view.k;
    draw();
  }

  var filter = document.getElementById("graph-filter");
  if (filter) filter.addEventListener("input", function () { q = filter.value.trim().toLowerCase(); if (!running) draw(); });
  var colorSel = document.getElementById("graph-color");
  if (colorSel) colorSel.addEventListener("change", function () { colorBy = colorSel.value; cat = {}; if (!running) draw(); });
  var reset = document.getElementById("graph-reset");
  if (reset) reset.addEventListener("click", fit);

  // re-theme on light/dark toggle
  var themeBtn = document.getElementById("theme-toggle");
  if (themeBtn) themeBtn.addEventListener("click", function () { setTimeout(function () { cat = {}; theme(); draw(); }, 0); });

  window.addEventListener("resize", resize);
  resize();
  run();
  setTimeout(fit, 600); // settle, then frame the whole graph
})();
