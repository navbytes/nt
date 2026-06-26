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
  import { showToast } from "../lib/toast.svelte";
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
  let userMoved = false; // user grabbed the 3D camera — suppress auto-refit
  let tick3d = 0; // 3D engine-tick counter, drives periodic re-fit while settling
  let lastRoot: string | null = null; // detects local-root changes to reset expansion
  let destroyed = false; // component torn down (guards async builds)
  let bloomPass: any = null; // three UnrealBloomPass (3D only)
  let three3d: any = null; // the lazy-loaded THREE namespace (3D only)
  let SpriteText3d: any = null; // lazy-loaded three-spritetext ctor (3D labels)
  let glowTex: any = null; // shared additive corona texture for 3D nodes (lazy)
  // Camera-adaptive glow (3D): the additive coronas are sized in WORLD space and the
  // fog fades by absolute distance, so the apparent glow swings from white-out (zoomed
  // in) to black-out (zoomed out). We counter-scale bloom strength + fog density by how
  // far the camera sits from the framed "neutral" distance to keep glow ~constant.
  const FOG_NEUTRAL = 0.00045; // fog density at the framed distance (was a flat 0.0009)
  let camRefDist = 0; // camera distance when the graph was first framed (neutral point)
  let camRefTO: ReturnType<typeof setTimeout> | undefined; // locks camRefDist post-fit
  // 3D field + label colors follow the page theme. Dark = a "deep space" near-black
  // field where the UnrealBloomPass + additive coronas read as luminous stars. Light
  // = a soft cool field with solid, legibly-coloured spheres; bloom + additive coronas
  // wash out to nothing on a bright background, so they're dropped in light mode (see
  // node3dObject + bloomStrength) and the sphere fill carries the colour instead.
  function bg3d(): string {
    return graphDark ? "#05060d" : "#eef1f7";
  }
  function label3dColor(): string {
    return graphDark ? "#e6e9f5" : cssFg; // light text on the dark field, dark on light
  }

  // Reduced-motion preference, kept live: read once for the initial value, then
  // updated by a matchMedia change listener (audit #27). Reads at imperative call
  // sites are non-reactive, but new calls pick up the current value.
  let reduce = $state(
    typeof window !== "undefined" &&
      (window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false),
  );

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
  // Dimmed-node fill alpha. A faint fill alone can't clear 3:1 vs the canvas
  // (even at 0.55 it's ~1.9–2.4:1), so de-emphasis is intentionally sub-threshold
  // here and dimmed nodes instead carry a thin FULL-strength outline (drawNode),
  // which keeps a ≥3:1 EDGE so they stay locatable (audit #4, approach a).
  const DIM = 0.38;
  // One-shot expanding ring on fresh selection (free-runs alongside the DOF tween).
  let pulseStart = 0;
  let pulseRAF = 0;

  // Pre-rendered glow sprites (graphSprites). Style is set from theme in readTheme.
  const sprites = new SpriteCache({ theme: "dark", glowAlpha: 0.6, glowBlur: 10 });

  // Label width cache. Canvas text width scales linearly with font size, so we
  // measure each label once at 1px and multiply — keeping ctx.measureText (a
  // layout op) out of the per-node, per-frame draw loop. Keyed by the displayed
  // label string; the set of short titles is small and stable per dataset.
  const labelW1 = new Map<string, number>();
  function labelWidth(ctx: CanvasRenderingContext2D, label: string, fontPx: number): number {
    let w = labelW1.get(label);
    if (w === undefined) {
      const prev = ctx.font;
      ctx.font = "1px ui-sans-serif, -apple-system, sans-serif";
      w = ctx.measureText(label).width;
      ctx.font = prev;
      labelW1.set(label, w);
    }
    return w * fontPx;
  }

  // Cached theme colors (refreshed on theme change) so the draw loop doesn't hit
  // getComputedStyle every frame.
  let cssFg = "#c0caf5";
  let cssBg = "#16161e";
  let cssAccent = "#7aa2f7";
  let cssGraphEdge = "rgba(0,0,0,0.5)";
  // Spectral identity (Aurora) — cached for the ambient canvas wash + focused edges.
  let cssSpectral1 = "#7aa2f7";
  let cssSpectral3 = "#bb9af7";
  // Reactive so the derived legends + colour recompute re-run on a theme switch
  // (the canvas draw loop reads it non-reactively, which is fine — repaint() is
  // pumped by the theme observer).
  let graphDark = $state(true);
  let cssChip = "#2c2c31"; // --bg-elevated (glass label chip fill, hex → alpha-able)
  let cssSep = "rgba(255,255,255,0.11)"; // --separator (already rgba)
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
    cssSpectral1 = cs.getPropertyValue("--spectral-1").trim() || cssSpectral1;
    cssSpectral3 = cs.getPropertyValue("--spectral-3").trim() || cssSpectral3;
    cssChip = cs.getPropertyValue("--bg-elevated").trim() || cssChip;
    cssSep = cs.getPropertyValue("--separator").trim() || cssSep;
    // theme is derived from the resolved bg luminance — covers the media-query
    // and [data-theme] paths uniformly without re-deriving the CSS selectors.
    const theme = isDarkHex(cssBg) ? "dark" : "light";
    graphDark = theme === "dark";
    const glowAlpha = parseFloat(cs.getPropertyValue("--graph-glow-alpha")) || 0.4;
    const glowBlur = parseFloat(cs.getPropertyValue("--graph-glow-blur")) || 8;
    sprites.setStyle({ theme, glowAlpha, glowBlur });
  }

  // ---- derived option lists for the controls panel ----
  const allNodes = $derived(($graphQ.data ? toForceGraph($graphQ.data).nodes : []) as FGNode[]);
  const folders = $derived([...new Set(allNodes.map((n) => n.folder).filter(Boolean))].sort());
  const tags = $derived([...new Set(allNodes.flatMap((n) => n.tags ?? []))].sort());
  const sources = $derived([...new Set(allNodes.map((n) => n.source).filter(Boolean))].sort());
  const legend = $derived(legendEntries(allNodes, view.colorBy, graphDark));
  const shapeLegend = $derived(shapeLegendEntries(allNodes, view.shapeBy));
  const edgeLegend = $derived(
    $graphQ.data ? linkKindLegend(($graphQ.data.links ?? []).map((l) => l.kind), graphDark) : [],
  );
  const selectedNode = $derived(allNodes.find((n) => n.id === view.selectedId) ?? null);
  // id → title lookup so the screen-reader fallback below can resolve neighbour
  // names in O(1) instead of an allNodes.find() per neighbour (was O(n²) on mount).
  const titleById = $derived(new Map(allNodes.map((n) => [n.id, n.title] as const)));
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
    // Reuse the memoized `allNodes` mapping instead of re-running toForceGraph
    // here — recompute fires on every search keystroke / filter change, so a fresh
    // O(n) node re-map per call was pure waste.
    const nodes = allNodes;
    const color = new Map<string, string>();
    const shape = new Map<string, ShapeKind>();
    const pass = new Set<string>();
    for (const n of nodes) {
      // Palette follows the page theme in both 2D and 3D (3D now has a light field
      // too): the bright palette reads on deep space, the light palette on the light field.
      color.set(n.id, colorOfNode(n, view.colorBy, graphDark));
      shape.set(n.id, nodeShape(n, view.shapeBy));
      if (passes(n)) pass.add(n.id);
    }
    R = { color, pass, shape };
    visibleCount = pass.size;
    refreshFx(); // pass-size may cross a perf threshold
    if (built3d) {
      refresh3dColors();
      refresh3dObjects(); // rebuild coronas/labels with fresh colors + filter state
    }
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
  // Link base grey is theme-aware: a dark grey on the light canvas, a light grey
  // on the dark canvas, both at an alpha high enough to clear ~2.5–3:1 vs the
  // canvas at rest (audit #3). #555 @0.7 ≈ 3.5:1 on white; #cfcfcf @0.7 ≈ 5.8:1
  // on #202024.
  function linkGrey(): string {
    return graphDark ? "207,207,207" : "85,85,85";
  }
  function linkBright(): string {
    return `rgba(${linkGrey()},0.7)`;
  }
  function linkDim(): string {
    return `rgba(${linkGrey()},0.32)`;
  }
  function linkHover(): string {
    return `rgba(${linkGrey()},0.95)`;
  }
  // linkAlpha mirrors nodeAlpha for edges (used by the gradient painter).
  function linkAlpha(s: string, t: string): number {
    if (!R.pass.has(s) || !R.pass.has(t)) return 0.32;
    if (focusSet && !(focusSet.has(s) && focusSet.has(t))) return 0.32 + 0.38 * (1 - dofT);
    return 0.7;
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

  // Rounded-rect path (manual — avoids relying on ctx.roundRect availability).
  function roundRect(
    ctx: CanvasRenderingContext2D,
    x: number,
    y: number,
    w: number,
    h: number,
    r: number,
  ) {
    ctx.beginPath();
    ctx.moveTo(x + r, y);
    ctx.arcTo(x + w, y, x + w, y + h, r);
    ctx.arcTo(x + w, y + h, x, y + h, r);
    ctx.arcTo(x, y + h, x, y, r);
    ctx.arcTo(x, y, x + w, y, r);
    ctx.closePath();
  }
  // Source→target gradient edges are the calm default whenever effects are on and
  // the graph isn't huge — flat grey is only the >2.5k / "off" perf fallback. This
  // keeps edges "living" even under the reduced-motion downgrade (a static gradient
  // isn't motion; only the animated depth-of-field is dropped there).
  function gradientLinks(): boolean {
    return view.colorLinks && !view.colorLinksByType && fxLevel !== "off" && R.pass.size <= 2500;
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
    // one-shot expanding ring pulse on fresh selection (reduced-motion: skipped)
    if (n.id === view.selectedId && pulseRAF) {
      const pp = Math.min(1, (performance.now() - pulseStart) / 600);
      ctx.beginPath();
      ctx.arc(x, y, r + 3 + pp * 15, 0, 2 * Math.PI);
      ctx.strokeStyle = withAlpha(cssAccent, (1 - pp) * 0.5);
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
    // Dimmed nodes carry a thin FULL-strength outline so they keep a ≥3:1 edge
    // against the canvas even though their fill is intentionally faint (audit #4).
    // (Orphans below draw their own outline; skip the double-stroke for them.)
    if (dim && n.deg !== 0) {
      tracePath(ctx, shape, x, y, r);
      ctx.lineWidth = 1 / scale;
      ctx.strokeStyle = base;
      ctx.stroke();
    }
    // orphans (no links) read as a hollow outline so they stand out — kept full
    // strength when dimmed too, for the same ≥3:1 edge.
    if (n.deg === 0) {
      tracePath(ctx, shape, x, y, r);
      ctx.lineWidth = 1 / scale;
      ctx.strokeStyle = base;
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
    // Hub labels (deg ≥ 5) are always-on, but only once moderately zoomed in —
    // at far-out scales every hub label fires at once and they pile up (audit #12).
    const hubLabel = n.deg >= 5 && scale > view.labelThreshold * 0.5;
    const show =
      view.showLabels &&
      !dim &&
      (scale > view.labelThreshold || n.id === hoveredId || n.id === view.selectedId || hubLabel);
    if (show) {
      const fontPx = Math.max(3.2, Math.min(6, 13 / scale));
      ctx.font = `${fontPx}px ui-sans-serif, -apple-system, sans-serif`;
      ctx.textAlign = "center";
      ctx.textBaseline = "middle";
      // Long task labels (an agent's whole sentence) sprawl across the canvas —
      // draw a short title; the full text stays in the hover tooltip (nodeLabel).
      const label = displayTitle(n.title, 20);
      // glass chip behind the label — a translucent elevated pill + hairline (on
      // brand + more legible over links/halos than the old stroke halo).
      const padX = 5 / scale;
      const chipH = fontPx + 5 / scale;
      const chipW = labelWidth(ctx, label, fontPx) + padX * 2;
      const cy = y + r + 3 / scale + chipH / 2;
      roundRect(ctx, x - chipW / 2, cy - chipH / 2, chipW, chipH, Math.min(chipH / 2, 4 / scale));
      ctx.fillStyle = withAlpha(cssChip, 0.72);
      ctx.fill();
      ctx.lineWidth = 0.5 / scale;
      ctx.strokeStyle = cssSep;
      ctx.stroke();
      ctx.fillStyle = cssFg;
      ctx.fillText(label, x, cy);
    }
  }

  // ---- links ----
  // Flat stroke accessor — used when the gradient painter is idle (Effects off,
  // or "Color links" off). Off mode keeps the original neutral grey exactly.
  function linkColorAccessor(l: any): string {
    const s = linkEndId(l.source);
    const t = linkEndId(l.target);
    if (hoveredId && (s === hoveredId || t === hoveredId)) return linkHover();
    const bright = linkAlpha(s, t) >= 0.7;
    // Relationship-type coloring wins over node-color when enabled.
    if (view.colorLinksByType) {
      return withAlpha(linkKindColor(l.kind, graphDark), bright ? 0.85 : 0.18);
    }
    if (view.colorLinks && fxLevel !== "off") {
      return withAlpha(R.color.get(s) ?? cssAccent, bright ? 0.7 : 0.18);
    }
    return bright ? linkBright() : linkDim();
  }

  // Gradient painter — replaces the link stroke only in "full" + colorLinks mode.
  // Arrows + particles still render (force-graph paints them in separate passes).
  // Reads link.__controlPoints (set by force-graph this frame) to follow the curve.
  function drawLink(l: any, ctx: CanvasRenderingContext2D, scale: number) {
    // The node-color gradient painter is idle when edges are colored by type
    // (a typed edge is one flat color, painted by the linkColor accessor instead).
    if (!gradientLinks()) return;
    const s = l.source;
    const t = l.target;
    if (!s || !t || s.x == null || t.x == null) return;
    const sid = linkEndId(s);
    const tid = linkEndId(t);
    const hov = hoveredId === sid || hoveredId === tid;
    const a = hov ? 0.85 : linkAlpha(sid, tid);
    const csrc = R.color.get(sid) ?? cssAccent;
    const ctgt = R.color.get(tid) ?? cssAccent;
    // Cache the gradient on the link: building it (createLinearGradient + two
    // withAlpha color-parses) is the per-link, per-frame cost that made the 2D
    // view burn CPU while a node was selected (flow particles force a continuous
    // 60fps redraw of a static layout). Gradients are interpreted in user space at
    // paint time, so a cached one stays correct across pan/zoom — only endpoint
    // motion, colour, or alpha changes invalidate it. Links are rebuilt on data
    // change, so the cache can't outlive its dataset.
    const key = `${s.x},${s.y},${t.x},${t.y},${csrc},${ctgt},${a}`;
    let grad: CanvasGradient = l.__grad;
    if (l.__gradKey !== key) {
      grad = ctx.createLinearGradient(s.x, s.y, t.x, t.y);
      grad.addColorStop(0, withAlpha(csrc, a));
      grad.addColorStop(1, withAlpha(ctgt, a));
      l.__grad = grad;
      l.__gradKey = key;
    }
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
  // The three gradients are screen-fixed (independent of pan/zoom), so they only
  // change on resize or theme — cache them rather than rebuilding three radial
  // gradients on every frame of the continuous (particle-driven) redraw.
  let vigKey = "";
  let vigGrads: [CanvasGradient, CanvasGradient, CanvasGradient] | null = null;
  function vignette(ctx: CanvasRenderingContext2D) {
    if (!container) return;
    const dpr = window.devicePixelRatio || 1;
    const w = container.clientWidth;
    const h = container.clientHeight;
    ctx.save();
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    // Aurora wash — two soft spectral glows pooled in opposite corners, matching
    // the app's ambient aurora. Screen-fixed, painted under the graph; kept subtle
    // (fainter in light mode) so nodes + labels stay legible.
    const key = `${w}x${h}:${graphDark}:${cssSpectral1}:${cssSpectral3}:${cssGraphEdge}`;
    if (vigKey !== key || !vigGrads) {
      const aa = graphDark ? 0.13 : 0.06;
      const reach = Math.max(w, h) * 0.55;
      const a1 = ctx.createRadialGradient(w * 0.82, h * 0.12, 0, w * 0.82, h * 0.12, reach);
      a1.addColorStop(0, withAlpha(cssSpectral1, aa));
      a1.addColorStop(1, withAlpha(cssSpectral1, 0));
      const a2 = ctx.createRadialGradient(w * 0.14, h * 0.94, 0, w * 0.14, h * 0.94, reach);
      a2.addColorStop(0, withAlpha(cssSpectral3, aa));
      a2.addColorStop(1, withAlpha(cssSpectral3, 0));
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
      vigGrads = [a1, a2, g];
      vigKey = key;
    }
    for (const grad of vigGrads) {
      ctx.fillStyle = grad;
      ctx.fillRect(0, 0, w, h);
    }
    ctx.restore();
  }

  // ---- cluster halos (community hulls) ----
  // Soft spectral blobs pooled behind same-colour clusters so structure reads at a
  // glance (the "hull" technique). Computed from live node positions — cheap and
  // throttled, never the full per-frame cost — and painted in world coords so each
  // halo pans/zooms with its cluster.
  let halos: { x: number; y: number; r: number; color: string }[] = [];
  let haloTick = 0;
  function refreshHalos() {
    if (!view.hulls || view.colorBy === "none" || fxLevel === "off" || !graph) {
      halos = [];
      return;
    }
    const nodes = graph.graphData?.().nodes ?? [];
    const groups = new Map<string, { sx: number; sy: number; n: number; pts: [number, number][] }>();
    for (const n of nodes) {
      if (!R.pass.has(n.id)) continue;
      const c = R.color.get(n.id);
      if (!c) continue;
      const x = n.x ?? 0;
      const y = n.y ?? 0;
      let g = groups.get(c);
      if (!g) {
        g = { sx: 0, sy: 0, n: 0, pts: [] };
        groups.set(c, g);
      }
      g.sx += x;
      g.sy += y;
      g.n++;
      g.pts.push([x, y]);
    }
    const next: typeof halos = [];
    for (const [color, g] of groups) {
      if (g.n < 2) continue; // a lone node isn't a community
      const cx = g.sx / g.n;
      const cy = g.sy / g.n;
      // mean (not max) distance → the blob hugs the cluster's dense core instead of
      // ballooning to a single far outlier (force layout spreads same-colour nodes).
      let sumd = 0;
      for (const [x, y] of g.pts) sumd += Math.hypot(x - cx, y - cy);
      const r = Math.max(60, Math.min((sumd / g.n) * 1.9 + 40, 520));
      next.push({ x: cx, y: cy, color, r });
    }
    halos = next;
  }
  // Recompute when the cache is cold or the layout is still moving; otherwise reuse
  // (positions are static once cooled). Bounds the per-frame cost on big graphs.
  function maybeRefreshHalos() {
    if (!view.hulls || view.colorBy === "none" || fxLevel === "off") {
      halos = [];
      return;
    }
    haloTick++;
    // Recompute only while the layout is actually moving (throttled), or when the
    // cache is cold. Once the engine stops, node positions are fixed, so the hulls
    // are constant — recomputing them every few frames during the particle-driven
    // continuous redraw was pure waste (an O(n) hypot pass per cluster).
    if (!halos.length || (engineRunning && haloTick % 4 === 0)) refreshHalos();
  }
  function drawHalos(ctx: CanvasRenderingContext2D) {
    if (!halos.length) return;
    const fade = focusSet ? 0.45 : 1; // recede while a node's web is in focus
    const base = (graphDark ? 0.24 : 0.13) * fade;
    for (const h of halos) {
      const g = ctx.createRadialGradient(h.x, h.y, 0, h.x, h.y, h.r);
      g.addColorStop(0, withAlpha(h.color, base));
      g.addColorStop(0.55, withAlpha(h.color, base * 0.5));
      g.addColorStop(1, withAlpha(h.color, 0));
      ctx.fillStyle = g;
      ctx.beginPath();
      ctx.arc(h.x, h.y, h.r, 0, 2 * Math.PI);
      ctx.fill();
    }
  }

  // ---- depth-of-field: animate the hover focus fade in/out ----
  function startDof(active: boolean) {
    if (built3d) return; // 3D conveys focus via depth/parallax, not canvas dimming
    // Focus the hovered node, or — when nothing is hovered — the selected node, so
    // clicking a node "lights its web": its ego-network brightens, the rest dims.
    const fid = hoveredId ?? view.selectedId;
    if (active && fid && adjacency.has(fid)) {
      focusSet = new Set<string>([fid, ...(adjacency.get(fid) ?? [])]);
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
          if (!pulseRAF) graph?.autoPauseRedraw?.(true);
          repaint();
        }
      };
      dofRAF = requestAnimationFrame(step);
    }
  }

  // A one-shot expanding ring on the selected node — a small "you picked this"
  // reward. Free-runs the rAF for ~600ms; coordinates autoPause with the DOF tween.
  function pulseSelect() {
    if (reduce || built3d) return;
    pulseStart = performance.now();
    if (pulseRAF) return;
    graph?.autoPauseRedraw?.(false);
    const step = () => {
      if (performance.now() - pulseStart < 600) {
        pulseRAF = requestAnimationFrame(step);
      } else {
        pulseRAF = 0;
        if (!dofRAF) graph?.autoPauseRedraw?.(true);
        repaint();
      }
    };
    pulseRAF = requestAnimationFrame(step);
  }

  // Particles flow only along the focused node's web (selecting/hovering "lights it
  // up"); with no focus, honour the global Flow toggle. 3D keeps the simple flow.
  function applyParticles() {
    if (!graph) return;
    if (built3d) {
      graph.linkDirectionalParticles(view.particles && !reduce ? 2 : 0);
      return;
    }
    graph.linkDirectionalParticles((l: any) => {
      if (reduce) return 0;
      if (focusSet && fxLevel === "full") {
        const s = linkEndId(l.source);
        const t = linkEndId(l.target);
        return focusSet.has(s) && focusSet.has(t) ? 3 : 0;
      }
      return view.particles ? 2 : 0;
    });
  }

  // ---- selection / navigation ----
  function selectNode(n: FGNode) {
    view.selectedId = n.id;
    if (built3d) {
      focusCamera3d(n);
    } else if (n.x != null && n.y != null) {
      const dur = reduce ? 0 : 450;
      pulseSelect();
      graph?.centerAt(n.x, n.y, dur);
      // gently zoom in to frame the node's neighborhood (never zoom back out)
      const cur = graph?.zoom?.() ?? 1;
      if (cur < 1.6) graph?.zoom(1.6, dur);
    }
  }

  let clickTO: ReturnType<typeof setTimeout> | undefined;
  function onNodeClick(n: FGNode, evt: MouseEvent) {
    closeMenu();
    // Cmd/Ctrl-click opens the note in a new tab (the web convention).
    if (evt?.metaKey || evt?.ctrlKey) {
      if (clickTO) clearTimeout(clickTO);
      window.open(n.url, "_blank", "noopener");
      return;
    }
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

  // ---- touch long-press → context menu (audit #10) ----
  // Touch/tablet users have no right-click, so a ~500ms press on a node opens the
  // same menu at the touch point. force-graph has no "node at point" API, so we
  // map the touch to graph coords and hit-test node positions by radius.
  let lpTimer: ReturnType<typeof setTimeout> | undefined;
  let lpStart: { x: number; y: number } | null = null;
  const LONG_PRESS_MS = 500;
  const LONG_PRESS_MOVE = 12; // px of drift that cancels the press (it's a pan)

  // Nearest node within its hit radius to a client (screen) point, or null.
  function nodeAtClient(clientX: number, clientY: number): FGNode | null {
    if (!graph || built3d || !container) return null;
    const rect = container.getBoundingClientRect();
    const sx = clientX - rect.left;
    const sy = clientY - rect.top;
    const gp = graph.screen2GraphCoords?.(sx, sy);
    if (!gp) return null;
    const nodes: FGNode[] = graph.graphData?.().nodes ?? [];
    let best: FGNode | null = null;
    let bestD = Infinity;
    for (const n of nodes) {
      if (n.x == null || n.y == null) continue;
      const r = nodeRadius(n) + 4; // small touch slop
      const d = Math.hypot(n.x - gp.x, n.y - gp.y);
      if (d <= r && d < bestD) {
        bestD = d;
        best = n;
      }
    }
    return best;
  }
  function cancelLongPress() {
    if (lpTimer) clearTimeout(lpTimer);
    lpTimer = undefined;
    lpStart = null;
  }
  function onTouchStart(e: TouchEvent) {
    if (e.touches.length !== 1) return cancelLongPress();
    const t = e.touches[0]!;
    lpStart = { x: t.clientX, y: t.clientY };
    cancelLongPressTimerOnly();
    lpTimer = setTimeout(() => {
      lpTimer = undefined;
      const start = lpStart;
      if (!start) return;
      const node = nodeAtClient(start.x, start.y);
      if (node) menu = { x: start.x, y: start.y, node };
    }, LONG_PRESS_MS);
  }
  function cancelLongPressTimerOnly() {
    if (lpTimer) clearTimeout(lpTimer);
    lpTimer = undefined;
  }
  function onTouchMove(e: TouchEvent) {
    if (!lpStart) return;
    const t = e.touches[0];
    if (!t) return;
    if (Math.hypot(t.clientX - lpStart.x, t.clientY - lpStart.y) > LONG_PRESS_MOVE) {
      cancelLongPress();
    }
  }
  function onTouchEnd() {
    cancelLongPress();
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
  // Copy with toast feedback + an insecure-context fallback (navigator.clipboard
  // is undefined off https/localhost, so the optional-chained writeText silently
  // no-op'd before — audit #25).
  async function copyText(text: string, label: string) {
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
        showToast(`Copied ${label}`);
        return;
      }
      throw new Error("clipboard unavailable");
    } catch {
      try {
        const ta = document.createElement("textarea");
        ta.value = text;
        ta.style.position = "fixed";
        ta.style.opacity = "0";
        document.body.appendChild(ta);
        ta.focus();
        ta.select();
        const ok = document.execCommand("copy");
        document.body.removeChild(ta);
        showToast(ok ? `Copied ${label}` : `Couldn't copy ${label}`);
      } catch {
        showToast(`Couldn't copy ${label}`);
      }
    }
  }
  function ctxCopyLink() {
    if (menu) void copyText(`[[${menu.node.title}]]`, "wikilink");
    closeMenu();
  }
  function ctxCopyPath() {
    if (menu) void copyText(menu.node.url, "note path");
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
      // Menu owns its own Escape dismissal (GraphContextMenu's window handler), so
      // it's not handled here — see audit #34.
      if (typing && view.search) return (view.search = "");
      if (view.selectedId) return (view.selectedId = null);
      if (view.mode === "local") return exitLocal();
      return;
    }
    // Keyboard entry to the context menu for the selected node (ContextMenu key or
    // Shift+F10). Positioned at the canvas centre.
    if (!typing && !menu && (e.key === "ContextMenu" || (e.shiftKey && e.key === "F10"))) {
      const sel = selectedNode;
      if (sel) {
        e.preventDefault();
        const rect = container?.getBoundingClientRect();
        const x = rect ? rect.left + rect.width / 2 : window.innerWidth / 2;
        const y = rect ? rect.top + rect.height / 2 : window.innerHeight / 2;
        menu = { x, y, node: sel };
      }
      return;
    }
    if (!typing && (e.key === "f" || e.key === "F")) {
      e.preventDefault();
      fit();
      return;
    }
    // Enter opens the selected node (keyboard activation from the focused canvas).
    if (!typing && e.key === "Enter" && selectedNode) {
      e.preventDefault();
      navigate(selectedNode.url);
      return;
    }
    if (!typing && e.key.startsWith("Arrow")) {
      e.preventDefault();
      // No selection yet → seed one at the most-connected visible node so arrow
      // navigation has a starting point (audit #5).
      if (!view.selectedId) {
        const def = defaultNode();
        if (def) selectNode(def);
        return;
      }
      moveSelection(e.key);
    }
  }

  // Highest-degree visible node — the sensible default selection when the canvas
  // is focused or an arrow is pressed with nothing selected.
  function defaultNode(): FGNode | null {
    let best: FGNode | null = null;
    for (const n of allNodes) {
      if (!R.pass.has(n.id)) continue;
      if (!best || n.deg > best.deg) best = n;
    }
    return best;
  }
  function onCanvasFocus() {
    if (!view.selectedId) {
      const def = defaultNode();
      if (def) selectNode(def);
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
      return near ? base : withAlpha(base, 0.3);
    }
    return R.pass.has(n.id) ? base : withAlpha(base, 0.3); // dimmed = recede
  }
  // Base link grey for 3D — a light blue-grey on the dark field, a darker blue-grey on
  // the light field (the dark-field grey is invisible on a bright background).
  function link3dGrey(a: number): string {
    return graphDark ? `rgba(160,166,190,${a})` : `rgba(96,103,128,${a})`;
  }
  function link3dColor(l: any): string {
    const s = linkEndId(l.source);
    const t = linkEndId(l.target);
    if (hoveredId) {
      if (s !== hoveredId && t !== hoveredId) return link3dGrey(0.15);
      if (view.colorLinksByType) return withAlpha(linkKindColor(l.kind, graphDark), 0.9);
      if (view.colorLinks) return withAlpha(R.color.get(s) ?? cssAccent, 0.85);
      return link3dGrey(0.85);
    }
    const bright = R.pass.has(s) && R.pass.has(t);
    if (view.colorLinksByType) return withAlpha(linkKindColor(l.kind, graphDark), bright ? 0.7 : 0.18);
    if (view.colorLinks) return withAlpha(R.color.get(s) ?? cssAccent, bright ? 0.6 : 0.18);
    return link3dGrey(bright ? 0.5 : 0.18);
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
  // Shared soft-dot texture for the additive node "corona" (built once, lazily).
  function glowTexture(): any {
    if (glowTex || !three3d) return glowTex;
    const c = document.createElement("canvas");
    c.width = c.height = 64;
    const g = c.getContext("2d");
    if (!g) return null;
    const rad = g.createRadialGradient(32, 32, 0, 32, 32, 32);
    rad.addColorStop(0, "rgba(255,255,255,1)");
    rad.addColorStop(0.4, "rgba(255,255,255,0.5)");
    rad.addColorStop(1, "rgba(255,255,255,0)");
    g.fillStyle = rad;
    g.fillRect(0, 0, 64, 64);
    glowTex = new three3d.CanvasTexture(c);
    return glowTex;
  }
  // node3dObject builds each node's extras on top of the default sphere: an additive
  // glow corona (so the bloom pass renders nodes as luminous "stars"), a text label
  // for hubs / selected / pinned, and a translucent halo for the selected node.
  function node3dObject(n: FGNode): any {
    if (!three3d) return undefined;
    const group = new three3d.Group();
    // glowing corona — additive sprite the UnrealBloomPass blooms into a star. Additive
    // blending only reads against the dark field; on the light field it washes out, so
    // light mode skips it and the solid sphere fill carries the colour instead.
    if (view.effects !== "off" && graphDark) {
      const spr = new three3d.Sprite(
        new three3d.SpriteMaterial({
          map: glowTexture(),
          color: R.color.get(n.id) ?? cssAccent,
          transparent: true,
          opacity: R.pass.has(n.id) ? 0.85 : 0.12,
          blending: three3d.AdditiveBlending,
          depthWrite: false,
        }),
      );
      const s = node3dRadius(n) * 2.6;
      spr.scale.set(s, s, 1);
      group.add(spr);
    }
    const labelOn =
      view.showLabels && (n.deg >= 5 || n.id === view.selectedId || pinned.has(n.id));
    if (labelOn && SpriteText3d) {
      const s = new SpriteText3d(displayTitle(n.title, 24));
      s.color = label3dColor(); // light on the dark field, dark (cssFg) on the light field
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
  // 3D mutation coalescing. Re-setting any of three-forcegraph's accessors forces
  // its digest to walk every node/link and recreate geometries (nodeThreeObject →
  // rebuild all node objects incl. SpriteText label textures; nodeColor/linkColor →
  // recreate all link lines). Hover (every mousemove), search (every keystroke) and
  // selection can each fire these in tight bursts, which is what makes the 3D view
  // lag and the page go unresponsive. Collapse all requests in a frame into a single
  // digest pass (objects supersede colors, since rebuilding objects re-reads color).
  const FX_COLORS = 1;
  const FX_OBJECTS = 2;
  let pending3d = 0;
  let raf3d = 0;
  function flush3d() {
    raf3d = 0;
    const f = pending3d;
    pending3d = 0;
    if (!graph || !built3d) return;
    if (f & FX_OBJECTS) graph.nodeThreeObject((n: FGNode) => node3dObject(n));
    if (f & FX_COLORS) {
      graph.nodeColor((n: FGNode) => node3dColor(n));
      graph.linkColor((l: any) => link3dColor(l));
    }
  }
  function schedule3d(flags: number) {
    if (!graph || !built3d) return;
    pending3d |= flags;
    if (!raf3d) raf3d = requestAnimationFrame(flush3d);
  }
  // Re-apply the node objects (labels + selection halo). Called when selection /
  // labels / sizing change — rebuilds the per-node three objects.
  function refresh3dObjects() {
    schedule3d(FX_OBJECTS);
  }
  function bloomStrength(): number {
    // Additive bloom washes out to nothing on the bright light field (every pixel is
    // already near-white), so the light theme renders solid spheres with no bloom.
    if (!graphDark) return 0;
    return view.effects === "off" ? 0 : view.effects === "subtle" ? 1.0 : 1.6;
  }
  // Keep the 3D glow legible across the whole zoom range. The bloom + additive coronas
  // read as a white-out when the camera is close (world-sized coronas flood the frame)
  // and the distance fog blacks everything out when it pulls back — both scale with
  // camera distance, so we counter-scale bloom strength and fog density against the
  // framed "neutral" distance. Cheap (a few math ops) → safe to run on every
  // OrbitControls change. Until the opening fit locks camRefDist we apply the base look.
  function tuneGlowForCamera() {
    if (!graph || !built3d) return;
    const base = bloomStrength();
    const cam = graph.camera?.();
    const fog = graph.scene?.()?.fog;
    if (!cam || !camRefDist) {
      if (bloomPass) bloomPass.strength = base;
      if (fog) fog.density = FOG_NEUTRAL;
      return;
    }
    const d = Math.hypot(cam.position.x, cam.position.y, cam.position.z);
    const rel = (d || camRefDist) / camRefDist; // >1 zoomed out, <1 zoomed in
    // Damp the bloom as you approach (kills the white-out), restore it — capped — as
    // you pull back so distant nodes still read as luminous stars.
    if (bloomPass) bloomPass.strength = base * Math.min(1.4, Math.max(0.3, rel));
    // Fog as a RELATIVE depth cue: track the zoom so the near side stays crisp and only
    // the far side hazes, instead of the whole field fading to the background (black-out).
    if (fog) fog.density = FOG_NEUTRAL * Math.min(1.8, Math.max(0.45, 1 / rel));
  }
  // 3D: re-set the color accessors with fresh closures so three rebuilds the
  // materials after a filter / colorBy / hover change (2D just repaints). Coalesced
  // through schedule3d so a burst of hover events collapses to one digest per frame.
  function refresh3dColors() {
    schedule3d(FX_COLORS);
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

  // The 3D renderer needs a WebGL context. Some embedded webviews — notably
  // WebKitGTK, which backs the Linux desktop app — don't expose one, and
  // 3d-force-graph then renders a blank canvas with no error. Probe for it up
  // front so we can fall back to the 2D canvas (which needs no WebGL) instead of
  // failing silently. The probe context is released immediately.
  function webglAvailable(): boolean {
    try {
      const c = document.createElement("canvas");
      const gl =
        c.getContext("webgl2") || c.getContext("webgl") || c.getContext("experimental-webgl");
      if (!gl) return false;
      (gl as WebGLRenderingContext).getExtension("WEBGL_lose_context")?.loseContext();
      return true;
    } catch {
      return false;
    }
  }

  // buildGraph (re)constructs the renderer for the active view.dim. It tears down
  // any previous instance so the 2D ⟷ 3D toggle can swap them in the same <div>.
  async function buildGraph() {
    if (!container || destroyed) return;
    // Drop any queued 3D digest from the renderer we're about to tear down.
    if (raf3d) cancelAnimationFrame(raf3d);
    raf3d = 0;
    pending3d = 0;
    // Drop the camera-adaptive-glow state from the old renderer (the 2D⟷3D swap and
    // data reloads reframe the graph, so the neutral distance must be re-locked).
    if (camRefTO) clearTimeout(camRefTO);
    camRefTO = undefined;
    camRefDist = 0;
    try {
      graph?._destructor?.();
    } catch {
      /* best-effort teardown */
    }
    graph = null;
    bloomPass = null;
    container.innerHTML = "";
    userMoved = false; // fresh build → allow the settle-fit until the user grabs the camera
    tick3d = 0;

    // Decide the renderer up front so a 3D failure can fall through to 2D within
    // this same call (no recursion, no rebuild race with the dim effect).
    let render3d = view.dim === "3d";
    if (render3d && !webglAvailable()) {
      showToast("3D graph needs WebGL, which isn't available here — showing 2D.");
      view.dim = "2d";
      render3d = false;
    }

    if (render3d) {
      try {
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
        .backgroundColor(bg3d())
        .showNavInfo(false)
        // Disable 3D node-drag. On drag-end — which an ordinary click often registers
        // as a micro-drag — 3d-force-graph dispatches a synthetic touch `pointerup` to
        // stop the orbit controls "taking over", and three 0.184's OrbitControls throws
        // on it (reads _pointerPositions for an untracked pointer → "Cannot read
        // properties of undefined (reading 'x')"). We don't use 3D drag (only the 2D
        // branch has an onNodeDragEnd handler), so turning it off removes the crash with
        // zero feature loss — orbit, click, and hover all still work.
        .enableNodeDrag(false)
        // Pre-spread the layout off-screen before the first paint, then settle
        // briefly and stop. The 3D engine otherwise inflates from the origin for
        // ~15s of charge repulsion; framing that growing cloud each tick dollied
        // the camera back continuously, so the graph appeared to shrink the whole
        // time. Warming up first means it opens at ~final scale and a single fit
        // frames it. (Mirrors the 2D reduced-motion warmup.)
        .warmupTicks(120)
        .cooldownTicks(120)
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
          tick3d++;
          // The constellation charges apart over the whole cooldown, so a single
          // early fit ends up far too close — stuck zoomed into the central node.
          // Track the inflating layout with periodic instant fits (not every
          // frame — that reads as constant shrinking) until the user grabs the
          // camera. The final settle-fit + glow lock happen in onEngineStop.
          if (!userMoved && (tick3d === 1 || tick3d % 15 === 0)) {
            graph?.zoomToFit?.(0, 60);
          }
        })
        .onEngineStop(() => {
          engineRunning = false;
          if (!userMoved && !destroyed) {
            // Frame the now-settled layout, then lock the neutral glow distance so
            // the adaptive bloom/fog measure zoom relative to the framed view.
            graph?.zoomToFit?.(reduce ? 0 : 400, 60);
            if (camRefTO) clearTimeout(camRefTO);
            camRefTO = setTimeout(() => {
              const cam = graph?.camera?.();
              if (cam) camRefDist = Math.hypot(cam.position.x, cam.position.y, cam.position.z);
              tuneGlowForCamera();
            }, reduce ? 0 : 450);
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
          g.scene().fog = new THREE.FogExp2(bg3d(), FOG_NEUTRAL);
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
          // Re-tune the glow whenever the camera zooms/orbits (fires on every move).
          ctrls.addEventListener?.("change", tuneGlowForCamera);
        }
      } catch {
        /* controls not ready — the effect below re-applies */
      }
      // A real user gesture on the canvas (drag-orbit or wheel-zoom) means they
      // own the camera now — suppress the settle-fit so it never yanks their
      // view. `once` self-removes the listener (we only need the first gesture)
      // so they don't accumulate across 2D⟷3D rebuilds.
      container.addEventListener("pointerdown", () => (userMoved = true), { once: true });
      container.addEventListener("wheel", () => (userMoved = true), { once: true, passive: true });
      built3d = true;
      graph = g;
      } catch (e) {
        // Dynamic import or WebGL init failed (older webview, blocked chunk,
        // lost context). Surface it and fall back to 2D so the graph is never
        // a silent blank pane.
        console.error("3D graph failed to initialise:", e);
        showToast("Couldn't load the 3D graph — showing 2D instead.");
        view.dim = "2d";
        render3d = false;
        try {
          graph?._destructor?.();
        } catch {
          /* best-effort teardown of the partial 3D renderer */
        }
        graph = null;
        if (container) container.innerHTML = "";
      }
    }

    if (!render3d) {
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
        .linkCanvasObjectMode(() => (gradientLinks() ? "replace" : "after"))
        .nodeCanvasObject(drawNode)
        .onRenderFramePre((ctx: CanvasRenderingContext2D) => {
          if (fxLevel !== "off") {
            vignette(ctx); // screen-fixed aurora wash + edge falloff (restores transform)
            maybeRefreshHalos();
            drawHalos(ctx); // world coords — over the aurora, behind the nodes/links
          }
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
          // Capture the hulls against the settled positions: maybeRefreshHalos no
          // longer recomputes once the engine is idle, so refresh once here.
          refreshHalos();
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
      if (lpTimer) clearTimeout(lpTimer);
      if (camRefTO) clearTimeout(camRefTO);
      if (dofRAF) cancelAnimationFrame(dofRAF);
      if (pulseRAF) cancelAnimationFrame(pulseRAF);
      if (raf3d) cancelAnimationFrame(raf3d);
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
      graphDark, // re-resolve the theme-correct palette on a theme switch (audit #1)
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

  // ---- clear a selection that's been filtered out of the visible graph (audit
  // #17). Selecting a node and then narrowing the filters (or leaving local scope)
  // could leave view.selectedId pointing at a node that's no longer shown. Read the
  // filter fields + visibleCount so this re-runs after recompute rebuilds R.pass. ----
  $effect(() => {
    void [
      view.search,
      view.filterFolders,
      view.filterTags,
      view.filterSources,
      view.hideOrphans,
      view.mode,
      view.depth,
      view.rootId,
      visibleCount,
    ];
    const id = view.selectedId;
    if (!id) return;
    const node = allNodes.find((n) => n.id === id);
    // Gone from the dataset entirely, or present but no longer in scope/passing.
    if (!node || !R.pass.has(id)) view.selectedId = null;
  });

  // ---- node sizing pref → repaint (matters when the sim is frozen) ----
  $effect(() => {
    void view.sizeBy;
    repaint();
  });

  // ---- depth-of-field: hover drives the focus fade (selection keeps its ring) ----
  $effect(() => {
    void [hoveredId, view.selectedId];
    // 3D: re-tint node/link materials for the hover neighborhood highlight.
    // 2D: animate the depth-of-field for the hovered OR selected node's web, and
    // re-scope the flow particles to that web.
    if (built3d) refresh3dColors();
    else {
      startDof((hoveredId ?? view.selectedId) != null);
      applyParticles();
    }
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
    tuneGlowForCamera(); // bloom strength scaled to the current zoom (not just base)
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
    void view.particles; // re-apply when the global Flow toggle changes
    applyParticles();
  });

  // ---- redraw on label-pref change (matters when the sim is frozen) ----
  $effect(() => {
    void [view.showLabels, view.labelThreshold, view.hulls, pinned];
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
      view.hulls,
    ];
    savePersisted();
  });

  // ---- reduced-motion preference: keep `reduce` live (audit #27) ----
  $effect(() => {
    const mq = window.matchMedia?.("(prefers-reduced-motion: reduce)");
    if (!mq) return;
    const onChange = () => {
      reduce = mq.matches;
      refreshFx(); // reduced-motion downgrades the effective fx level
      repaint();
    };
    mq.addEventListener?.("change", onChange);
    return () => mq.removeEventListener?.("change", onChange);
  });

  // ---- theme changes: refresh cached colors + canvas bg ----
  $effect(() => {
    if (typeof MutationObserver === "undefined") return;
    const onTheme = () => {
      readTheme(); // sets graphDark → the graphDark-tracked recompute re-colours nodes/objects
      if (built3d) {
        // 3D: swap the field + fog to the theme's colour and re-tune bloom (0 in light).
        // The palette + coronas/labels are rebuilt by the recompute effect (it tracks
        // graphDark); here we own only what recompute doesn't — bg, fog colour, bloom.
        graph?.backgroundColor(bg3d());
        const fog = graph?.scene?.()?.fog;
        if (fog?.color) fog.color.set(bg3d());
        tuneGlowForCamera();
      } else {
        graph?.backgroundColor(cssBg);
      }
      repaint();
    };
    const obs = new MutationObserver(onTheme);
    obs.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme", "class"] });
    const mq = window.matchMedia?.("(prefers-color-scheme: dark)");
    mq?.addEventListener?.("change", onTheme);
    return () => {
      obs.disconnect();
      mq?.removeEventListener?.("change", onTheme);
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

  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div
    class="graph-canvas"
    bind:this={container}
    tabindex="0"
    role="application"
    aria-label="Knowledge graph. Use arrow keys to move between connected notes, Enter to open."
    onfocus={onCanvasFocus}
    ontouchstart={onTouchStart}
    ontouchmove={onTouchMove}
    ontouchend={onTouchEnd}
    ontouchcancel={onTouchEnd}
  ></div>

  <!-- Screen-reader live region + structured fallback (canvas is invisible to AT). -->
  <div class="sr-only" aria-live="polite">{announce}</div>
  {#if allNodes.length}
    <ul class="sr-only" aria-label="Notes and their links">
      {#each allNodes as n (n.id)}
        <li>
          <a href={n.url}>{n.title}</a>
          {#if adjacency.get(n.id)?.size}
            — links to {[...(adjacency.get(n.id) ?? [])]
              .map((id) => titleById.get(id))
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
    /* Full-bleed past .main's padding (var(--space-7/8/10) = 24/32/48). The
       HORIZONTAL bleed must equal .main's side padding exactly (32px) — any more
       and the stage is wider than .main, which overflows the page (a small
       horizontal scrollbar). Top/bottom over-bleed is harmless (clipped + offscreen). */
    margin: -28px -32px -64px;
    /* 100dvh follows mobile browser chrome (toolbars) where 100vh overshoots
       (audit #9). Desktop layout is unchanged (dvh == vh with no dynamic chrome). */
    height: calc(100dvh - 58px);
    overflow: hidden;
    /* Unify the wrapper background with the token force-graph paints into the
       canvas (cssBg = --bg = --bg-content), so there's ONE background — the CSS
       fill no longer differs from the painted canvas (audit #7). */
    background: var(--bg-content);
  }
  .graph-canvas {
    position: absolute;
    inset: 0;
    background: var(--bg-content);
  }
  /* Mobile: the desktop negative margins assume the desktop .main padding; on
     small screens .main uses smaller padding, so the bleed mismatches and the
     stage over/under-hangs. Neutralise the horizontal/bottom bleed and recompute
     the height against the mobile chrome (audit #9). Desktop is untouched. */
  @media (max-width: 760px) {
    .graph-stage {
      /* match the mobile .main padding (20px 16px 56px) so the bleed is exact */
      margin: -20px -16px -56px;
      height: calc(100dvh - 50px);
    }
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
