<script lang="ts">
  import { onMount } from "svelte";
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import { displayTitle } from "../lib/text";
  import { navigate } from "../lib/router.svelte";
  import {
    toForceGraph,
    buildAdjacency,
    nodesWithinDepth,
    linkEndId,
    type FGNode,
    type FGLink,
  } from "../lib/graph";
  import {
    nodeColor as colorOfNode,
    legendEntries,
    withAlpha,
    linkKindColor,
    linkKindLegend,
  } from "../lib/graphColors";
  import { bfsDepths, pageRank } from "../lib/graphMetrics";
  import { nodeShape, tracePath, shapeLegendEntries, type ShapeKind } from "../lib/graphShapes";
  import { SpriteCache, drawRadius, type SpriteVariant } from "../lib/graphSprites";
  import { view, savePersisted, enterLocal, exitLocal, type Effects } from "../lib/graphView.svelte";
  import GraphControls from "../lib/GraphControls.svelte";
  import GraphDetails from "../lib/GraphDetails.svelte";
  import GraphContextMenu from "../lib/GraphContextMenu.svelte";
  import Icon from "../lib/Icon.svelte";

  let { focus = "" }: { focus?: string } = $props();

  const graphQ = createQuery({ queryKey: ["graph"], queryFn: api.graph });

  let container: HTMLDivElement | undefined = $state();
  let graph = $state<any>(null);
  let hoveredId = $state<string | null>(null);
  let pinned = $state<Set<string>>(new Set());
  // Incremental expansion (local mode): nodes the user explicitly "expanded" to
  // pull their neighbors into view beyond the depth slider. Transient like pinned.
  let expanded = $state<Set<string>>(new Set());
  let menu = $state<{ x: number; y: number; node: FGNode } | null>(null);
  // Effective visual level after reduced-motion / size / layout downgrades
  // (computeFx). Read by the hot draw loop and the link/vignette wiring.
  let fxLevel = $state<Effects>("full");
  let engineRunning = $state(false);
  // renderer construction state (2D force-graph ⟷ 3D constellation)
  let built3d = false; // which renderer is currently mounted
  let built = false; // first build finished (gates dim-toggle rebuilds)
  let fitPending = false; // zoom-to-fit once the next layout settles
  let lastRoot: string | null = null; // detects local-root changes to reset expansion
  let destroyed = false; // component torn down (guards async builds)
  let bloomPass: any = null; // three UnrealBloomPass (3D only)
  let three3d: any = null; // the lazy-loaded THREE namespace (3D only)
  let SpriteText3d: any = null; // lazy-loaded three-spritetext ctor (3D labels)
  const BG3D = "#05060d"; // deep-space background — bloom needs near-black

  const reduce =
    typeof window !== "undefined" &&
    window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;

  // ---- precomputed render state (rebuilt on data/filter change, read as O(1)
  // lookups by the per-frame canvas accessors — never recomputed per frame).
  const adjacency = $derived(
    $graphQ.data ? buildAdjacency($graphQ.data) : new Map<string, Set<string>>(),
  );
  // Centrality (PageRank) for "size by importance". Computed once per dataset and
  // read O(1) by nodeRadius in the hot draw loop via the plain mirrors below.
  let prMap = new Map<string, number>();
  let prMax = 1;
  // R.pass = ids passing the active filters (persistent). Hover/selection
  // depth-of-field is layered on at draw time via focusSet + dofT, so it can
  // animate without rebuilding R on every hover.
  let R: { color: Map<string, string>; pass: Set<string>; shape: Map<string, ShapeKind> } = {
    color: new Map(),
    pass: new Set(),
    shape: new Map(),
  };

  // Depth-of-field: focusSet = hovered node + neighbors (kept through the fade-out
  // so un-dimming animates too); dofT ramps 0→1 = full-context → dimmed-context.
  let focusSet: Set<string> | null = null;
  let dofT = 0;
  let dofRAF = 0;
  let dofFrom = 0;
  let dofTo = 0;
  let dofStart = 0;
  const DOF_MS = 180;
  const DIM = 0.15; // dimmed-node alpha (matches the original base + "26")

  // Pre-rendered glow sprites (graphSprites). Style is set from theme in readTheme.
  const sprites = new SpriteCache({ theme: "dark", glowAlpha: 0.6, glowBlur: 10 });

  // Cached theme colors (refreshed on theme change) so the draw loop doesn't hit
  // getComputedStyle every frame.
  let cssFg = "#c0caf5";
  let cssBg = "#16161e";
  let cssAccent = "#7aa2f7";
  let cssGraphEdge = "rgba(0,0,0,0.5)";
  function isDarkHex(hex: string): boolean {
    const h = hex.replace("#", "");
    if (h.length < 6) return false;
    const n = parseInt(h.slice(0, 6), 16);
    if (Number.isNaN(n)) return false;
    return 0.299 * ((n >> 16) & 255) + 0.587 * ((n >> 8) & 255) + 0.114 * (n & 255) < 128;
  }
  function readTheme() {
    const cs = getComputedStyle(document.documentElement);
    cssFg = cs.getPropertyValue("--fg").trim() || cssFg;
    cssBg = cs.getPropertyValue("--bg").trim() || cssBg;
    cssAccent = cs.getPropertyValue("--accent").trim() || cssAccent;
    cssGraphEdge = cs.getPropertyValue("--graph-bg-edge").trim() || cssGraphEdge;
    // theme is derived from the resolved bg luminance — covers the media-query
    // and [data-theme] paths uniformly without re-deriving the CSS selectors.
    const theme = isDarkHex(cssBg) ? "dark" : "light";
    const glowAlpha = parseFloat(cs.getPropertyValue("--graph-glow-alpha")) || 0.4;
    const glowBlur = parseFloat(cs.getPropertyValue("--graph-glow-blur")) || 8;
    sprites.setStyle({ theme, glowAlpha, glowBlur });
  }

  // ---- derived option lists for the controls panel ----
  const allNodes = $derived(($graphQ.data ? toForceGraph($graphQ.data).nodes : []) as FGNode[]);
  const folders = $derived([...new Set(allNodes.map((n) => n.folder).filter(Boolean))].sort());
  const tags = $derived([...new Set(allNodes.flatMap((n) => n.tags ?? []))].sort());
  const sources = $derived([...new Set(allNodes.map((n) => n.source).filter(Boolean))].sort());
  const legend = $derived(legendEntries(allNodes, view.colorBy));
  const shapeLegend = $derived(shapeLegendEntries(allNodes, view.shapeBy));
  const edgeLegend = $derived(
    $graphQ.data ? linkKindLegend(($graphQ.data.links ?? []).map((l) => l.kind)) : [],
  );
  const selectedNode = $derived(allNodes.find((n) => n.id === view.selectedId) ?? null);
  // R is a plain (non-reactive) object read O(1) by the hot draw loop; the panel
  // reads this mirrored count instead so it stays reactive.
  let visibleCount = $state(0);

  // ---- filtering / scope ----
  function passes(n: FGNode): boolean {
    if (view.hideOrphans && n.deg === 0) return false;
    if (view.filterFolders.length && !view.filterFolders.includes(n.folder)) return false;
    if (view.filterSources.length && !view.filterSources.includes(n.source)) return false;
    if (view.filterTags.length && !(n.tags ?? []).some((t) => view.filterTags.includes(t))) return false;
    const q = view.search.trim().toLowerCase();
    if (q && !n.title.toLowerCase().includes(q)) return false;
    return true;
  }

  function scopedData(): { nodes: FGNode[]; links: FGLink[] } {
    const data = $graphQ.data;
    if (!data) return { nodes: [], links: [] };
    let { nodes, links } = toForceGraph(data);
    // "Show tasks" off → drop task nodes (and any edge touching one) entirely.
    if (!view.showTasks) {
      nodes = nodes.filter((n) => n.kind !== "task");
      const keep = new Set(nodes.map((n) => n.id));
      links = links.filter((l) => keep.has(linkEndId(l.source)) && keep.has(linkEndId(l.target)));
    }
    if (view.mode === "local" && view.rootId) {
      const within = nodesWithinDepth(view.rootId, adjacency, view.depth);
      // Incremental expansion: each explicitly-expanded node pulls in its direct
      // neighbors, so you can grow specific branches past the depth slider.
      for (const id of expanded) {
        if (!within.has(id) && !adjacency.has(id)) continue;
        within.add(id);
        for (const nb of adjacency.get(id) ?? []) within.add(nb);
      }
      nodes = nodes.filter((n) => within.has(n.id));
      links = links.filter((l) => within.has(linkEndId(l.source)) && within.has(linkEndId(l.target)));
    }
    return { nodes, links };
  }

  // recompute rebuilds the color + shape + pass sets once per relevant change.
  function recompute() {
    const data = $graphQ.data;
    if (!data) return;
    const nodes = toForceGraph(data).nodes;
    const color = new Map<string, string>();
    const shape = new Map<string, ShapeKind>();
    const pass = new Set<string>();
    for (const n of nodes) {
      color.set(n.id, colorOfNode(n, view.colorBy));
      shape.set(n.id, nodeShape(n, view.shapeBy));
      if (passes(n)) pass.add(n.id);
    }
    R = { color, pass, shape };
    visibleCount = pass.size;
    refreshFx(); // pass-size may cross a perf threshold
    if (built3d) refresh3dColors();
    repaint();
  }

  // ---- canvas helpers ----
  function nodeRadius(n: FGNode): number {
    if (view.sizeBy === "centrality") {
      // Normalize against the dataset's top score, then sqrt-scale so hubs read
      // bigger without the long tail collapsing to invisible dots.
      const s = (prMap.get(n.id) ?? 0) / (prMax || 1);
      return Math.max(2.5, Math.min(14, 3 + Math.sqrt(s) * 11));
    }
    return Math.min(12, Math.sqrt(1 + n.deg) * 3);
  }
  // nodeAlpha folds the two kinds of dimming: filtered-out nodes are always faint;
  // hover/selection context fades smoothly via dofT (the depth-of-field tween).
  function nodeAlpha(id: string): number {
    if (!R.pass.has(id)) return DIM;
    if (focusSet && !focusSet.has(id)) return 1 - (1 - DIM) * dofT;
    return 1;
  }
  const LINK_BRIGHT = "rgba(128,128,128,0.5)";
  const LINK_DIM = "rgba(128,128,128,0.08)";
  const LINK_HOVER = "rgba(128,128,128,0.85)";
  // linkAlpha mirrors nodeAlpha for edges (used by the gradient painter).
  function linkAlpha(s: string, t: string): number {
    if (!R.pass.has(s) || !R.pass.has(t)) return 0.08;
    if (focusSet && !(focusSet.has(s) && focusSet.has(t))) return 0.08 + 0.42 * (1 - dofT);
    return 0.5;
  }

  // force-graph v1.51 exposes no refresh(), and re-setting a style prop to its
  // current value is a no-op (kapsule skips equal values). Un-pausing the rAF
  // redraw loop for one frame reliably repaints even when cooled/frozen. The
  // dofRAF guard avoids stomping an in-flight depth-of-field tween (which manages
  // autoPauseRedraw itself).
  function repaint() {
    if (!graph || built3d) return; // 3D renders continuously; only 2D needs nudging
    graph.autoPauseRedraw(false);
    requestAnimationFrame(() => {
      if (!dofRAF) graph?.autoPauseRedraw(true);
    });
  }

  // computeFx downgrades the requested level for reduced-motion, large graphs, and
  // active layout so the hot loop stays affordable. Off = original flat look.
  function computeFx(): Effects {
    let fx = view.effects;
    const n = R.pass.size;
    if (reduce && fx === "full") fx = "subtle"; // glow stays, animated DOF off
    if (n > 5000) return "off"; // plain fill on very large graphs
    if (fx === "full" && (n > 2500 || engineRunning)) fx = "subtle"; // drop gradient + DOF
    return fx;
  }
  function refreshFx() {
    fxLevel = computeFx();
  }

  function drawNode(n: FGNode, ctx: CanvasRenderingContext2D, scale: number) {
    const x = n.x ?? 0;
    const y = n.y ?? 0;
    const r = nodeRadius(n);
    const base = R.color.get(n.id) ?? cssAccent;
    const shape = R.shape.get(n.id) ?? "circle";
    const alpha = nodeAlpha(n.id);
    const dim = alpha < 0.99;

    // root / selected ring
    if (n.id === view.rootId || n.id === view.selectedId) {
      ctx.beginPath();
      ctx.arc(x, y, r + 3, 0, 2 * Math.PI);
      ctx.strokeStyle = cssAccent;
      ctx.lineWidth = 1.5 / scale;
      ctx.stroke();
    }
    // node body
    if (fxLevel !== "off") {
      // glowing orb sprite (hovered / selected nodes bloom brighter via "focus")
      const variant: SpriteVariant =
        (focusSet?.has(n.id) ?? false) || n.id === view.selectedId ? "focus" : "bright";
      const sprite = sprites.get(shape, base, variant);
      const rd = drawRadius(r);
      ctx.save();
      if (alpha < 1) ctx.globalAlpha = alpha;
      ctx.drawImage(sprite, x - rd, y - rd, rd * 2, rd * 2);
      ctx.restore();
    } else {
      // flat fill — the "Effects: Off" fallback (pixel-equivalent to the original)
      tracePath(ctx, shape, x, y, r);
      ctx.fillStyle = dim ? withAlpha(base, alpha) : base;
      ctx.fill();
    }
    // orphans (no links) read as a hollow outline so they stand out
    if (n.deg === 0) {
      tracePath(ctx, shape, x, y, r);
      ctx.lineWidth = 1 / scale;
      ctx.strokeStyle = withAlpha(base, dim ? 0.33 : 1);
      ctx.stroke();
    }
    // pinned indicator
    if (pinned.has(n.id)) {
      ctx.beginPath();
      ctx.arc(x, y, r + 1.5, 0, 2 * Math.PI);
      ctx.strokeStyle = withAlpha(cssFg, dim ? 0.33 : 1);
      ctx.lineWidth = 0.8 / scale;
      ctx.setLineDash([2 / scale, 2 / scale]);
      ctx.stroke();
      ctx.setLineDash([]);
    }

    // label: past the zoom threshold, or always for hovered / selected / hubs.
    const show =
      view.showLabels &&
      !dim &&
      (scale > view.labelThreshold || n.id === hoveredId || n.id === view.selectedId || n.deg >= 5);
    if (show) {
      const fontPx = Math.max(3.2, Math.min(6, 13 / scale));
      ctx.font = `${fontPx}px sans-serif`;
      ctx.textAlign = "center";
      ctx.textBaseline = "top";
      const ty = y + r + 2 / scale;
      // Long task labels (an agent's whole sentence) sprawl across the canvas —
      // draw a short title; the full text stays in the hover tooltip (nodeLabel).
      const label = displayTitle(n.title, 20);
      // halo for legibility over links/nodes
      ctx.lineWidth = 3 / scale;
      ctx.strokeStyle = cssBg;
      ctx.strokeText(label, x, ty);
      ctx.fillStyle = cssFg;
      ctx.fillText(label, x, ty);
    }
  }

  // ---- links ----
  // Flat stroke accessor — used when the gradient painter is idle (Effects off,
  // or "Color links" off). Off mode keeps the original neutral grey exactly.
  function linkColorAccessor(l: any): string {
    const s = linkEndId(l.source);
    const t = linkEndId(l.target);
    if (hoveredId && (s === hoveredId || t === hoveredId)) return LINK_HOVER;
    const bright = linkAlpha(s, t) >= 0.5;
    // Relationship-type coloring wins over node-color when enabled.
    if (view.colorLinksByType) {
      return withAlpha(linkKindColor(l.kind), bright ? 0.7 : 0.12);
    }
    if (view.colorLinks && fxLevel !== "off") {
      return withAlpha(R.color.get(s) ?? cssAccent, bright ? 0.55 : 0.12);
    }
    return bright ? LINK_BRIGHT : LINK_DIM;
  }

  // Gradient painter — replaces the link stroke only in "full" + colorLinks mode.
  // Arrows + particles still render (force-graph paints them in separate passes).
  // Reads link.__controlPoints (set by force-graph this frame) to follow the curve.
  function drawLink(l: any, ctx: CanvasRenderingContext2D, scale: number) {
    // The node-color gradient painter is idle when edges are colored by type
    // (a typed edge is one flat color, painted by the linkColor accessor instead).
    if (!(view.colorLinks && !view.colorLinksByType && fxLevel === "full")) return;
    const s = l.source;
    const t = l.target;
    if (!s || !t || s.x == null || t.x == null) return;
    const sid = linkEndId(s);
    const tid = linkEndId(t);
    const hov = hoveredId === sid || hoveredId === tid;
    const a = hov ? 0.85 : linkAlpha(sid, tid);
    const grad = ctx.createLinearGradient(s.x, s.y, t.x, t.y);
    grad.addColorStop(0, withAlpha(R.color.get(sid) ?? cssAccent, a));
    grad.addColorStop(1, withAlpha(R.color.get(tid) ?? cssAccent, a));
    ctx.strokeStyle = grad;
    ctx.lineWidth = (hov ? 2 : 1) / scale;
    ctx.beginPath();
    ctx.moveTo(s.x, s.y);
    const cp = l.__controlPoints;
    if (!cp) ctx.lineTo(t.x, t.y);
    else if (cp.length === 2) ctx.quadraticCurveTo(cp[0], cp[1], t.x, t.y);
    else ctx.bezierCurveTo(cp[0], cp[1], cp[2], cp[3], t.x, t.y);
    ctx.stroke();
  }

  // Radial vignette painted under the graph each frame for depth. The frame ctx is
  // in world coords (zoom×dpr), so reset to CSS pixels to keep it screen-fixed.
  function vignette(ctx: CanvasRenderingContext2D) {
    if (!container) return;
    const dpr = window.devicePixelRatio || 1;
    const w = container.clientWidth;
    const h = container.clientHeight;
    ctx.save();
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    const g = ctx.createRadialGradient(
      w / 2,
      h / 2,
      Math.min(w, h) * 0.25,
      w / 2,
      h / 2,
      Math.hypot(w, h) / 2,
    );
    g.addColorStop(0, "rgba(0,0,0,0)");
    g.addColorStop(1, cssGraphEdge);
    ctx.fillStyle = g;
    ctx.fillRect(0, 0, w, h);
    ctx.restore();
  }

  // ---- depth-of-field: animate the hover focus fade in/out ----
  function startDof(active: boolean) {
    if (built3d) return; // 3D conveys focus via depth/parallax, not canvas dimming
    if (active && hoveredId && adjacency.has(hoveredId)) {
      focusSet = new Set<string>([hoveredId, ...(adjacency.get(hoveredId) ?? [])]);
    }
    const target = active ? 1 : 0;
    if (reduce || fxLevel !== "full") {
      dofT = target;
      if (!active) focusSet = null;
      repaint();
      return;
    }
    dofFrom = dofT;
    dofTo = target;
    dofStart = performance.now();
    if (!dofRAF) {
      graph?.autoPauseRedraw?.(false); // free-run the rAF loop during the tween
      const step = () => {
        const p = Math.min(1, (performance.now() - dofStart) / DOF_MS);
        const e = 1 - (1 - p) * (1 - p); // ease-out quad
        dofT = dofFrom + (dofTo - dofFrom) * e;
        if (p < 1) {
          dofRAF = requestAnimationFrame(step);
        } else {
          dofRAF = 0;
          if (dofTo === 0) focusSet = null;
          graph?.autoPauseRedraw?.(true);
          repaint();
        }
      };
      dofRAF = requestAnimationFrame(step);
    }
  }

  // ---- selection / navigation ----
  function selectNode(n: FGNode) {
    view.selectedId = n.id;
    if (built3d) {
      focusCamera3d(n);
    } else if (n.x != null && n.y != null) {
      graph?.centerAt(n.x, n.y, reduce ? 0 : 450);
    }
  }

  let clickTO: ReturnType<typeof setTimeout> | undefined;
  function onNodeClick(n: FGNode, evt: MouseEvent) {
    closeMenu();
    // Shift/Alt-click reveals a node's neighbors (incremental exploration) rather
    // than selecting it. Roots a local view first if we're still global.
    if (evt?.shiftKey || evt?.altKey) {
      if (clickTO) clearTimeout(clickTO);
      if (view.mode !== "local" || !view.rootId) enterLocal(n.id);
      else toggleExpand(n.id);
      return;
    }
    if (evt?.detail >= 2) {
      if (clickTO) clearTimeout(clickTO);
      navigate(n.url);
      return;
    }
    if (clickTO) clearTimeout(clickTO);
    clickTO = setTimeout(() => selectNode(n), 200);
  }

  // toggleExpand grows/ungrows a node's neighborhood in local mode (scopedData
  // unions the neighbors of every expanded node). Re-fits and reheats so the
  // newly-revealed nodes lay out.
  function toggleExpand(id: string) {
    const next = new Set(expanded);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    expanded = next;
    if (!view.frozen) graph?.d3ReheatSimulation?.();
  }

  function closeMenu() {
    menu = null;
  }

  // ---- pinning ----
  function togglePin(id: string) {
    const node = graph?.graphData().nodes.find((n: FGNode) => n.id === id) as FGNode | undefined;
    const next = new Set(pinned);
    if (next.has(id)) {
      next.delete(id);
      if (node) {
        node.fx = undefined;
        node.fy = undefined;
      }
      graph?.d3ReheatSimulation();
    } else {
      next.add(id);
      if (node) {
        node.fx = node.x;
        node.fy = node.y;
      }
    }
    pinned = next;
  }

  function fit() {
    graph?.zoomToFit(reduce ? 0 : 450, 40);
  }

  // ---- context-menu actions ----
  function ctxOpen() {
    if (menu) navigate(menu.node.url);
    closeMenu();
  }
  function ctxOpenNewTab() {
    if (menu) window.open(menu.node.url, "_blank", "noopener");
    closeMenu();
  }
  function ctxFocusLocal() {
    if (menu) enterLocal(menu.node.id);
    closeMenu();
  }
  function ctxTogglePin() {
    if (menu) togglePin(menu.node.id);
    closeMenu();
  }
  function ctxCopyLink() {
    if (menu) navigator.clipboard?.writeText(`[[${menu.node.title}]]`);
    closeMenu();
  }
  function ctxCopyPath() {
    if (menu) navigator.clipboard?.writeText(menu.node.url);
    closeMenu();
  }

  // ---- keyboard ----
  function moveSelection(key: string) {
    const sel = selectedNode;
    if (!sel || sel.x == null) return;
    const want =
      key === "ArrowRight" ? 0 : key === "ArrowDown" ? 90 : key === "ArrowLeft" ? 180 : 270;
    let best: FGNode | null = null;
    let bestDelta = 91;
    const nodes: FGNode[] = graph?.graphData().nodes ?? [];
    for (const id of adjacency.get(sel.id) ?? []) {
      const nb = nodes.find((n) => n.id === id);
      if (!nb || nb.x == null || nb.y == null) continue;
      const ang = (Math.atan2(nb.y - sel.y!, nb.x - sel.x!) * 180) / Math.PI;
      let delta = Math.abs(((ang - want + 540) % 360) - 180);
      if (delta < bestDelta) {
        bestDelta = delta;
        best = nb;
      }
    }
    if (best && bestDelta < 80) selectNode(best);
  }

  function onKey(e: KeyboardEvent) {
    const el = e.target as HTMLElement | null;
    const typing =
      el?.tagName === "INPUT" || el?.tagName === "TEXTAREA" || el?.isContentEditable === true;
    if (e.key === "/" && !typing) {
      e.preventDefault();
      document.getElementById("graph-search")?.focus();
      return;
    }
    if (e.key === "Escape") {
      if (menu) return closeMenu();
      if (typing && view.search) return (view.search = "");
      if (view.selectedId) return (view.selectedId = null);
      if (view.mode === "local") return exitLocal();
      return;
    }
    if (!typing && (e.key === "f" || e.key === "F")) {
      e.preventDefault();
      fit();
      return;
    }
    if (!typing && view.selectedId && e.key.startsWith("Arrow")) {
      e.preventDefault();
      moveSelection(e.key);
    }
  }

  // ---- live-region announcement for screen readers ----
  const announce = $derived(
    selectedNode
      ? `Selected ${selectedNode.title}, folder ${selectedNode.folder || "root"}, ${selectedNode.deg} connections`
      : "",
  );

  // ---- build the force graph ----
  function size() {
    if (graph && container) graph.width(container.clientWidth).height(container.clientHeight);
  }

  // ---- 3D renderer helpers (sphere + line materials, bloom, camera) ----
  // Hovering focuses the neighborhood (like the 2D depth-of-field): the hovered
  // node + its neighbors stay lit while everything else recedes hard.
  function node3dColor(n: FGNode): string {
    const base = R.color.get(n.id) ?? cssAccent;
    if (hoveredId) {
      const near = n.id === hoveredId || (adjacency.get(hoveredId)?.has(n.id) ?? false);
      return near ? base : withAlpha(base, 0.05);
    }
    return R.pass.has(n.id) ? base : withAlpha(base, 0.1); // dimmed = recede
  }
  function link3dColor(l: any): string {
    const s = linkEndId(l.source);
    const t = linkEndId(l.target);
    if (hoveredId) {
      if (s !== hoveredId && t !== hoveredId) return "rgba(160,166,190,0.03)";
      if (view.colorLinksByType) return withAlpha(linkKindColor(l.kind), 0.9);
      if (view.colorLinks) return withAlpha(R.color.get(s) ?? cssAccent, 0.85);
      return "rgba(190,196,220,0.85)";
    }
    const bright = R.pass.has(s) && R.pass.has(t);
    if (view.colorLinksByType) return withAlpha(linkKindColor(l.kind), bright ? 0.7 : 0.08);
    if (view.colorLinks) return withAlpha(R.color.get(s) ?? cssAccent, bright ? 0.6 : 0.08);
    return bright ? "rgba(160,166,190,0.5)" : "rgba(160,166,190,0.08)";
  }
  // node3dVal drives the sphere volume. Honors the "Size by" pref so centrality
  // (PageRank) hubs read bigger in 3D too, matching the 2D radius logic.
  function node3dVal(n: FGNode): number {
    if (view.sizeBy === "centrality") {
      const s = (prMap.get(n.id) ?? 0) / (prMax || 1);
      return 1 + s * 60; // emphasize hubs in the volume scale
    }
    return 1 + n.deg;
  }
  // Approximate the rendered sphere radius (3d-force-graph: nodeRelSize · ∛val,
  // default nodeRelSize = 4) so labels/halos can be offset clear of the node.
  function node3dRadius(n: FGNode): number {
    return 4 * Math.cbrt(node3dVal(n));
  }
  // node3dObject adds (on top of the default sphere) a text label for hubs /
  // selected / pinned nodes and a translucent halo for the selected node, so the
  // 3D view gains the readability + selection cues the 2D canvas already has.
  function node3dObject(n: FGNode): any {
    if (!three3d) return undefined;
    const group = new three3d.Group();
    const labelOn =
      view.showLabels && (n.deg >= 5 || n.id === view.selectedId || pinned.has(n.id));
    if (labelOn && SpriteText3d) {
      const s = new SpriteText3d(displayTitle(n.title, 24));
      s.color = cssFg;
      s.backgroundColor = false;
      s.textHeight = 5;
      s.position.y = node3dRadius(n) + 6;
      group.add(s);
    }
    if (n.id === view.selectedId) {
      const geom = new three3d.SphereGeometry(node3dRadius(n) + 5, 16, 16);
      const mat = new three3d.MeshBasicMaterial({
        color: cssAccent,
        transparent: true,
        opacity: 0.18,
      });
      group.add(new three3d.Mesh(geom, mat));
    }
    return group.children.length ? group : undefined;
  }
  // Re-apply the node objects (labels + selection halo). Called when selection /
  // labels / sizing change — rebuilds the per-node three objects.
  function refresh3dObjects() {
    if (!graph || !built3d) return;
    graph.nodeThreeObject((n: FGNode) => node3dObject(n));
  }
  function bloomStrength(): number {
    return view.effects === "off" ? 0 : view.effects === "subtle" ? 1.1 : 2.0;
  }
  // 3D: re-set the color accessors with fresh closures so three rebuilds the
  // materials after a filter / colorBy change (2D just repaints).
  function refresh3dColors() {
    graph?.nodeColor?.((n: FGNode) => node3dColor(n));
    graph?.linkColor?.((l: any) => link3dColor(l));
  }
  // Fly the 3D camera to frame a node (2D uses centerAt instead).
  function focusCamera3d(n: FGNode) {
    const x = (n as any).x ?? 0;
    const y = (n as any).y ?? 0;
    const z = (n as any).z ?? 0;
    const ratio = 1 + 130 / (Math.hypot(x, y, z) || 1);
    graph?.cameraPosition?.({ x: x * ratio, y: y * ratio, z: z * ratio }, n, reduce ? 0 : 800);
  }

  // ---- radial / ego layout (2D) ----
  // Radial needs a root, so it only engages in local mode on the 2D renderer.
  function radialActive(): boolean {
    return (
      view.layout === "radial" && view.mode === "local" && !!view.rootId && !built3d && !!graph
    );
  }
  // applyRadial installs (or removes) a d3 forceRadial that pulls each node onto a
  // ring whose radius is its hop-distance from the root, turning the ego graph
  // into clean concentric circles. The centering forces are zeroed while it's on
  // (they'd fight the rings) and the root is pinned at the origin.
  async function applyRadial() {
    if (!graph || built3d) return;
    const on = radialActive();
    const nodes: FGNode[] = graph.graphData().nodes;
    const root = view.rootId ? nodes.find((n) => n.id === view.rootId) : undefined;
    if (on) {
      const { forceRadial } = await import("d3-force");
      if (!graph || !radialActive()) return; // re-check after the await
      const depthMap = bfsDepths(view.rootId!, adjacency);
      const maxD = Math.max(1, ...depthMap.values());
      const ring = Math.max(40, view.linkDistance * 2.4);
      graph.d3Force(
        "radial",
        forceRadial((n: FGNode) => (depthMap.get(n.id) ?? maxD + 1) * ring, 0, 0).strength(0.9),
      );
      graph.d3Force("x")?.strength(0);
      graph.d3Force("y")?.strength(0);
      if (root) {
        root.fx = 0;
        root.fy = 0;
      }
    } else {
      graph.d3Force("radial", null); // remove
      graph.d3Force("x")?.strength(view.centerGravity);
      graph.d3Force("y")?.strength(view.centerGravity);
      if (root && view.rootId && !pinned.has(view.rootId) && !view.frozen) {
        root.fx = undefined;
        root.fy = undefined;
      }
    }
    if (!view.frozen) graph.d3ReheatSimulation();
  }

  // buildGraph (re)constructs the renderer for the active view.dim. It tears down
  // any previous instance so the 2D ⟷ 3D toggle can swap them in the same <div>.
  async function buildGraph() {
    if (!container || destroyed) return;
    try {
      graph?._destructor?.();
    } catch {
      /* best-effort teardown */
    }
    graph = null;
    bloomPass = null;
    container.innerHTML = "";

    if (view.dim === "3d") {
      const { default: ForceGraph3D } = await import("3d-force-graph");
      // @ts-ignore - three 0.184 ships no bundled type declarations; only Vector2 is used
      const THREE: any = await import("three");
      const { default: SpriteText } = await import("three-spritetext");
      if (!container || destroyed) return;
      three3d = THREE;
      SpriteText3d = SpriteText;
      const g: any = new ForceGraph3D(container, { controlType: "orbit" });
      g.nodeId("id")
        .nodeVal((n: FGNode) => node3dVal(n))
        .nodeLabel((n: FGNode) => n.title)
        .nodeColor((n: FGNode) => node3dColor(n))
        .nodeOpacity(0.92)
        .nodeResolution(14)
        .nodeThreeObjectExtend(true)
        .nodeThreeObject((n: FGNode) => node3dObject(n))
        .linkColor((l: any) => link3dColor(l))
        .linkOpacity(0.55)
        .linkWidth(0)
        .linkCurvature(view.linkStyle === "curved" ? 0.18 : 0)
        .backgroundColor(BG3D)
        .showNavInfo(false)
        .onNodeHover((n: FGNode | null) => {
          hoveredId = n?.id ?? null;
          if (container) container.style.cursor = n ? "pointer" : "";
        })
        .onNodeClick((n: FGNode, evt: MouseEvent) => onNodeClick(n, evt))
        .onNodeRightClick((n: FGNode, evt: MouseEvent) => {
          evt.preventDefault?.();
          menu = { x: evt.clientX, y: evt.clientY, node: n };
        })
        .onBackgroundClick(() => {
          closeMenu();
          view.selectedId = null;
        })
        .onEngineTick(() => {
          if (!engineRunning) engineRunning = true;
        })
        .onEngineStop(() => {
          engineRunning = false;
          if (fitPending) {
            fitPending = false;
            fit();
          }
        });

      // Bloom — the additive glow that makes the constellation read as premium.
      try {
        // @ts-ignore - three 0.184 ships no type declarations for this addons subpath
        const { UnrealBloomPass } = await import("three/examples/jsm/postprocessing/UnrealBloomPass.js"); // prettier-ignore
        const bloom = new UnrealBloomPass(new THREE.Vector2(1, 1), bloomStrength(), 0.85, 0.05);
        g.postProcessingComposer().addPass(bloom);
        bloomPass = bloom;
      } catch (e) {
        console.warn("3D bloom unavailable:", e);
      }

      // Depth fog: distant nodes fade into the background, a strong depth cue on
      // big constellations (skipped in the flat "Effects: off" look).
      if (view.effects !== "off") {
        try {
          g.scene().fog = new THREE.FogExp2(BG3D, 0.0009);
        } catch (e) {
          console.warn("3D fog unavailable:", e);
        }
      }
      // Ambient auto-rotate (OrbitControls), off by default.
      try {
        const ctrls = g.controls?.();
        if (ctrls) {
          ctrls.autoRotate = view.autoRotate;
          ctrls.autoRotateSpeed = 0.6;
        }
      } catch {
        /* controls not ready — the effect below re-applies */
      }
      built3d = true;
      graph = g;
    } else {
      const { default: ForceGraph } = await import("force-graph");
      if (!container || destroyed) return;
      const g = new ForceGraph<FGNode, FGLink>(container)
        .nodeId("id")
        .nodeVal((n: FGNode) => 1 + n.deg)
        .nodeLabel((n: FGNode) => n.title)
        .backgroundColor(cssBg)
        // Painting happens in nodeCanvasObject (drawNode); force-graph ignores
        // nodeColor once a canvas-object accessor is set, so it's omitted here.
        .linkColor(linkColorAccessor)
        .linkWidth((l: any) =>
          hoveredId && (linkEndId(l.source) === hoveredId || linkEndId(l.target) === hoveredId)
            ? 2
            : 1,
        )
        .linkCurvature(view.linkStyle === "curved" ? 0.12 : 0)
        // Gradient painter replaces the stroke only in "full" + colorLinks mode
        // (mode "after" elsewhere → drawLink no-ops and the flat stroke shows).
        .linkCanvasObject(drawLink)
        .linkCanvasObjectMode(() =>
          view.colorLinks && !view.colorLinksByType && fxLevel === "full" ? "replace" : "after",
        )
        .nodeCanvasObject(drawNode)
        .onRenderFramePre((ctx: CanvasRenderingContext2D) => {
          if (fxLevel !== "off") vignette(ctx);
        })
        .nodePointerAreaPaint((n: FGNode, color: string, ctx: CanvasRenderingContext2D) => {
          ctx.beginPath();
          ctx.arc(n.x ?? 0, n.y ?? 0, nodeRadius(n) + 2, 0, 2 * Math.PI);
          ctx.fillStyle = color;
          ctx.fill();
        })
        .onNodeHover((n: FGNode | null) => {
          hoveredId = n?.id ?? null;
          if (container) container.style.cursor = n ? "pointer" : "";
        })
        .onNodeClick(onNodeClick)
        .onNodeRightClick((n: FGNode, evt: MouseEvent) => {
          evt.preventDefault?.();
          menu = { x: evt.clientX, y: evt.clientY, node: n };
        })
        .onNodeDragEnd((n: FGNode) => {
          n.fx = n.x;
          n.fy = n.y;
          pinned = new Set(pinned).add(n.id);
        })
        .onBackgroundClick((evt: MouseEvent) => {
          closeMenu();
          view.selectedId = null;
          if (evt?.detail >= 2) fit();
        })
        .onBackgroundRightClick((evt: MouseEvent) => {
          evt.preventDefault?.();
          closeMenu();
        })
        .onEngineTick(() => {
          if (!engineRunning) engineRunning = true;
        })
        .onEngineStop(() => {
          engineRunning = false;
          if (fitPending) {
            fitPending = false;
            fit();
          }
        });

      // Gentle positional forces toward the origin keep disconnected nodes
      // (orphans) from drifting off to infinity — d3's centering force only
      // recenters the mean, it doesn't anchor individual components. The
      // "Center gravity" slider drives their strength.
      const { forceX, forceY } = await import("d3-force");
      g.d3Force("x", forceX(0).strength(view.centerGravity));
      g.d3Force("y", forceY(0).strength(view.centerGravity));

      if (reduce) g.warmupTicks(80).cooldownTicks(0);
      built3d = false;
      graph = g;
    }

    built = true;
    size();
    fitPending = true;
    graph.graphData(scopedData());
    recompute();
  }

  onMount(() => {
    let ro: ResizeObserver | undefined;
    readTheme();
    void buildGraph().then(() => {
      if (container && !destroyed) {
        ro = new ResizeObserver(size);
        ro.observe(container);
      }
    });
    return () => {
      destroyed = true;
      if (clickTO) clearTimeout(clickTO);
      if (dofRAF) cancelAnimationFrame(dofRAF);
      ro?.disconnect();
      try {
        graph?._destructor?.();
      } catch {
        /* best-effort teardown */
      }
      graph = null;
    };
  });

  // ---- honor deep-link focus once the graph data (and adjacency) is ready ----
  $effect(() => {
    if (focus && adjacency.has(focus) && view.rootId !== focus) enterLocal(focus);
  });

  // ---- feed (scoped) data on mode/depth/root/data change ----
  $effect(() => {
    const data = $graphQ.data;
    // establish dependencies
    void [view.mode, view.depth, view.rootId, view.showTasks, data];
    if (!graph || !data) return;
    // Reset incremental expansion when the scope changes (new root / back to
    // global) so stale neighborhoods don't linger. Guarded by size so clearing
    // doesn't re-trigger this effect (scopedData reads `expanded`).
    if (view.rootId !== lastRoot) {
      lastRoot = view.rootId;
      if (expanded.size) expanded = new Set();
    }
    if (view.mode === "global" && expanded.size) expanded = new Set();
    fitPending = true;
    graph.graphData(scopedData());
    // local mode roots a fresh layout — clear stale pins from the previous scope
    if (view.mode === "global") pinned = new Set();
  });

  // ---- recompute color/shape/pass sets on filter/colorBy change ----
  // (hover/selection is handled by the depth-of-field effect below, not here.)
  $effect(() => {
    void [
      $graphQ.data,
      view.colorBy,
      view.shapeBy,
      view.search,
      view.filterFolders,
      view.filterTags,
      view.filterSources,
      view.hideOrphans,
      view.mode,
      view.depth,
      view.rootId,
    ];
    recompute();
  });

  // ---- centrality (PageRank): recompute once per dataset for "size by importance" ----
  $effect(() => {
    void $graphQ.data;
    prMap = pageRank(adjacency);
    prMax = prMap.size ? Math.max(...prMap.values()) : 1;
    repaint();
  });

  // ---- node sizing pref → repaint (matters when the sim is frozen) ----
  $effect(() => {
    void view.sizeBy;
    repaint();
  });

  // ---- depth-of-field: hover drives the focus fade (selection keeps its ring) ----
  $effect(() => {
    void hoveredId;
    // 3D: re-tint node/link materials for the hover neighborhood highlight.
    // 2D: animate the canvas depth-of-field.
    if (built3d) refresh3dColors();
    else startDof(hoveredId !== null);
  });

  // ---- keep the effective level current on effects-pref / layout change ----
  $effect(() => {
    void [view.effects, engineRunning];
    refreshFx();
  });

  // ---- 2D link styling: curvature + gradient on/off → repaint (matters when frozen) ----
  $effect(() => {
    void [view.linkStyle, view.colorLinks, view.colorLinksByType, fxLevel];
    if (!graph || built3d) return;
    graph.linkCurvature(view.linkStyle === "curved" ? 0.12 : 0);
    repaint();
  });

  // ---- 3D styling: bloom strength + link color/curvature on pref change ----
  $effect(() => {
    void [view.effects, view.colorLinks, view.colorLinksByType, view.linkStyle, view.dim];
    if (!graph || !built3d) return;
    if (bloomPass) bloomPass.strength = bloomStrength();
    graph.linkColor((l: any) => link3dColor(l));
    graph.linkCurvature(view.linkStyle === "curved" ? 0.18 : 0);
  });

  // ---- 3D node objects: rebuild labels + selection halo on selection/label change ----
  $effect(() => {
    void [view.selectedId, view.showLabels, pinned];
    if (built3d) refresh3dObjects();
  });

  // ---- 3D sizing: re-apply node volume + objects when "Size by" / data changes ----
  $effect(() => {
    void [view.sizeBy, $graphQ.data];
    if (!graph || !built3d) return;
    graph.nodeVal((n: FGNode) => node3dVal(n));
    refresh3dObjects();
  });

  // ---- 3D auto-rotate toggle ----
  $effect(() => {
    void [view.autoRotate, view.dim];
    if (!graph || !built3d) return;
    const ctrls = graph.controls?.();
    if (ctrls) {
      ctrls.autoRotate = view.autoRotate;
      ctrls.autoRotateSpeed = 0.6;
    }
  });

  // ---- rebuild the renderer when the 2D/3D toggle flips ----
  $effect(() => {
    const want3d = view.dim === "3d";
    if (built && want3d !== built3d) void buildGraph();
  });

  // ---- physics ----
  $effect(() => {
    if (!graph) return;
    const charge = graph.d3Force("charge");
    if (charge?.strength) charge.strength(view.repel);
    const link = graph.d3Force("link");
    if (link?.distance) link.distance(view.linkDistance);
    // Radial mode owns the x/y centering forces (zeroed) — don't fight it here.
    const grav = radialActive() ? 0 : view.centerGravity;
    graph.d3Force("x")?.strength(grav);
    graph.d3Force("y")?.strength(grav);
    if (!view.frozen) graph.d3ReheatSimulation();
  });

  // ---- radial / ego layout: (re)apply when layout, scope, depth, or data change ----
  $effect(() => {
    void [view.layout, view.mode, view.rootId, view.depth, view.linkDistance, $graphQ.data, built];
    if (!graph || built3d) return;
    void applyRadial();
  });

  // ---- freeze: fix all nodes in place (keeps full interactivity, stops jitter) ----
  $effect(() => {
    if (!graph) return;
    const frozen = view.frozen;
    const nodes: FGNode[] = graph.graphData().nodes;
    for (const n of nodes) {
      if (frozen) {
        n.fx = n.x;
        n.fy = n.y;
        (n as any).fz = (n as any).z;
      } else if (!pinned.has(n.id)) {
        n.fx = undefined;
        n.fy = undefined;
        (n as any).fz = undefined;
      }
    }
    if (!frozen) graph.d3ReheatSimulation();
  });

  // ---- arrows + particle flow ----
  $effect(() => {
    if (!graph) return;
    graph.linkDirectionalArrowLength(view.showArrows ? 3.5 : 0);
    graph.linkDirectionalArrowRelPos(0.9);
    graph.linkDirectionalParticles(view.particles && !reduce ? 2 : 0);
  });

  // ---- redraw on label-pref change (matters when the sim is frozen) ----
  $effect(() => {
    void [view.showLabels, view.labelThreshold, pinned];
    repaint();
  });

  // ---- persist look/physics prefs ----
  $effect(() => {
    void [
      view.colorBy,
      view.shapeBy,
      view.showLabels,
      view.showArrows,
      view.particles,
      view.repel,
      view.linkDistance,
      view.centerGravity,
      view.labelThreshold,
      view.depth,
      view.effects,
      view.linkStyle,
      view.colorLinks,
      view.colorLinksByType,
      view.dim,
      view.layout,
      view.sizeBy,
      view.autoRotate,
    ];
    savePersisted();
  });

  // ---- theme changes: refresh cached colors + canvas bg ----
  $effect(() => {
    if (typeof MutationObserver === "undefined") return;
    const obs = new MutationObserver(() => {
      readTheme();
      graph?.backgroundColor(cssBg);
      repaint();
    });
    obs.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme", "class"] });
    const mq = window.matchMedia?.("(prefers-color-scheme: dark)");
    const onMq = () => {
      readTheme();
      graph?.backgroundColor(cssBg);
      repaint();
    };
    mq?.addEventListener?.("change", onMq);
    return () => {
      obs.disconnect();
      mq?.removeEventListener?.("change", onMq);
    };
  });
</script>

<svelte:window onkeydown={onKey} />

<div class="graph-stage">
  {#if $graphQ.isPending}
    <div class="graph-overlay">
      <div class="graph-msg">
        <span class="graph-msg__art graph-msg__art--load"><Icon name="graph" size={26} /></span>
        <p class="graph-msg__eyebrow">Knowledge graph</p>
        <p class="graph-msg__lead">Mapping your notes…</p>
      </div>
    </div>
  {:else if $graphQ.error}
    <div class="graph-overlay">
      <div class="graph-msg">
        <span class="graph-msg__art graph-msg__art--err"><Icon name="warning" size={24} /></span>
        <p class="graph-msg__lead">Couldn't load the graph</p>
        <p class="muted">Something went wrong reaching the store.</p>
      </div>
    </div>
  {:else if allNodes.length === 0}
    <div class="graph-overlay">
      <div class="graph-msg">
        <span class="graph-msg__art graph-msg__art--empty"><Icon name="graph" size={26} /></span>
        <p class="graph-msg__eyebrow">Knowledge graph</p>
        <p class="graph-msg__lead">No graph yet</p>
        <p class="muted">Create notes and connect them with <code>[[wikilinks]]</code> to see your graph grow.</p>
      </div>
    </div>
  {:else}
    <GraphControls
      {folders}
      {tags}
      {sources}
      {legend}
      {shapeLegend}
      {edgeLegend}
      nodeCount={allNodes.length}
      linkCount={$graphQ.data?.links.length ?? 0}
      {visibleCount}
      onFit={fit}
    />

    {#if $graphQ.data?.truncated && view.mode === "global"}
      <div class="graph-banner">
        Showing the {allNodes.length} most-connected notes. Select a node and switch to <strong>Local</strong> to explore the rest.
      </div>
    {:else if allNodes.length > 1500 && view.mode === "global"}
      <div class="graph-banner">
        Large graph ({allNodes.length} notes). Select a node and switch to <strong>Local</strong> for a faster, clearer view.
      </div>
    {/if}

    {#if selectedNode}
      <GraphDetails
        node={selectedNode}
        pinned={pinned.has(selectedNode.id)}
        expanded={expanded.has(selectedNode.id)}
        onOpen={() => selectedNode && navigate(selectedNode.url)}
        onFocusLocal={() => selectedNode && enterLocal(selectedNode.id)}
        onTogglePin={() => selectedNode && togglePin(selectedNode.id)}
        onToggleExpand={() => {
          if (!selectedNode) return;
          if (view.mode !== "local" || !view.rootId) enterLocal(selectedNode.id);
          else toggleExpand(selectedNode.id);
        }}
        onClose={() => (view.selectedId = null)}
      />
    {/if}

    {#if menu}
      <GraphContextMenu
        x={menu.x}
        y={menu.y}
        node={menu.node}
        pinned={pinned.has(menu.node.id)}
        onOpen={ctxOpen}
        onOpenNewTab={ctxOpenNewTab}
        onFocusLocal={ctxFocusLocal}
        onTogglePin={ctxTogglePin}
        onCopyLink={ctxCopyLink}
        onCopyPath={ctxCopyPath}
        onClose={closeMenu}
      />
    {/if}
  {/if}

  <div class="graph-canvas" bind:this={container}></div>

  <!-- Screen-reader live region + structured fallback (canvas is invisible to AT). -->
  <div class="sr-only" aria-live="polite">{announce}</div>
  {#if allNodes.length}
    <ul class="sr-only" aria-label="Notes and their links">
      {#each allNodes as n (n.id)}
        <li>
          <a href={n.url}>{n.title}</a>
          {#if adjacency.get(n.id)?.size}
            — links to {[...(adjacency.get(n.id) ?? [])]
              .map((id) => allNodes.find((m) => m.id === id)?.title)
              .filter(Boolean)
              .join(", ")}
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .graph-stage {
    position: relative;
    margin: -28px -36px -64px;
    height: calc(100vh - 58px);
    overflow: hidden;
    background: var(--bg-inset);
  }
  .graph-canvas {
    position: absolute;
    inset: 0;
  }
  .graph-overlay {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 10;
  }
  /* Centered editorial message for the loading / empty / error states — mono
     eyebrow over a serif lead, with a spectral-tinted icon medallion. Matches
     the app's .empty--hero language. */
  .graph-msg {
    text-align: center;
    max-width: 360px;
    padding: 0 var(--space-5);
  }
  .graph-msg__art {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 64px;
    height: 64px;
    margin: 0 auto var(--space-4);
    border-radius: 50%;
    color: var(--spectral-2);
    background:
      linear-gradient(
        135deg,
        color-mix(in srgb, var(--spectral-1) 18%, transparent),
        color-mix(in srgb, var(--spectral-3) 18%, transparent)
      );
    box-shadow: var(--glow-spectral);
  }
  /* loading medallion gently breathes (transform/opacity only; reduced-motion
     neutralizes it via the global rule). */
  .graph-msg__art--load {
    animation: graph-pulse 2.4s var(--ease) infinite;
  }
  @keyframes graph-pulse {
    0%,
    100% {
      opacity: 0.75;
      transform: scale(1);
    }
    50% {
      opacity: 1;
      transform: scale(1.06);
    }
  }
  .graph-msg__art--err {
    color: var(--red);
    background: color-mix(in srgb, var(--red) 13%, transparent);
    box-shadow: none;
  }
  .graph-msg__eyebrow {
    margin: 0 0 var(--space-1);
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    color: var(--accent-color);
  }
  .graph-msg__lead {
    font-family: var(--font-serif);
    font-size: 1.3rem;
    font-weight: 600;
    letter-spacing: -0.01em;
    margin: 0 0 var(--space-2);
    color: var(--fg);
  }
  /* Glass info banner (truncation / large-graph notice) — translucent pill with
     the float shadow + light-catch hairline, floated over the canvas. */
  .graph-banner {
    position: absolute;
    top: 12px;
    left: 50%;
    transform: translateX(-50%);
    z-index: 15;
    max-width: min(560px, 80vw);
    background: color-mix(in srgb, var(--bg-elevated) 86%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    border: 0.5px solid var(--separator);
    border-radius: 999px;
    padding: 7px 16px;
    font-size: var(--text-callout);
    color: var(--label-secondary);
    box-shadow: var(--shadow-float), var(--glass-hairline);
    text-align: center;
  }
  .graph-banner strong {
    color: var(--fg);
  }
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }
</style>
