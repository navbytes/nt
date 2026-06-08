<script lang="ts">
  import { onMount } from "svelte";
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import { navigate } from "../lib/router.svelte";
  import {
    toForceGraph,
    buildAdjacency,
    nodesWithinDepth,
    linkEndId,
    type FGNode,
    type FGLink,
  } from "../lib/graph";
  import { nodeColor as colorOfNode, legendEntries } from "../lib/graphColors";
  import { nodeShape, tracePath, shapeLegendEntries, type ShapeKind } from "../lib/graphShapes";
  import { view, savePersisted, enterLocal, exitLocal } from "../lib/graphView.svelte";
  import GraphControls from "../lib/GraphControls.svelte";
  import GraphDetails from "../lib/GraphDetails.svelte";
  import GraphContextMenu from "../lib/GraphContextMenu.svelte";

  let { focus = "" }: { focus?: string } = $props();

  const graphQ = createQuery({ queryKey: ["graph"], queryFn: api.graph });

  let container: HTMLDivElement | undefined = $state();
  let graph = $state<any>(null);
  let hoveredId = $state<string | null>(null);
  let pinned = $state<Set<string>>(new Set());
  let menu = $state<{ x: number; y: number; node: FGNode } | null>(null);

  const reduce =
    typeof window !== "undefined" &&
    window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;

  // ---- precomputed render state (rebuilt on data/filter/hover change, read as
  // O(1) lookups by the per-frame canvas accessors — never recomputed per frame).
  const adjacency = $derived(
    $graphQ.data ? buildAdjacency($graphQ.data) : new Map<string, Set<string>>(),
  );
  let R: { color: Map<string, string>; bright: Set<string>; shape: Map<string, ShapeKind> } = {
    color: new Map(),
    bright: new Set(),
    shape: new Map(),
  };

  // Cached theme colors (refreshed on theme change) so the draw loop doesn't hit
  // getComputedStyle every frame.
  let cssFg = "#c0caf5";
  let cssBg = "#16161e";
  let cssAccent = "#7aa2f7";
  function readTheme() {
    const cs = getComputedStyle(document.documentElement);
    cssFg = cs.getPropertyValue("--fg").trim() || cssFg;
    cssBg = cs.getPropertyValue("--bg").trim() || cssBg;
    cssAccent = cs.getPropertyValue("--accent").trim() || cssAccent;
  }

  // ---- derived option lists for the controls panel ----
  const allNodes = $derived(($graphQ.data ? toForceGraph($graphQ.data).nodes : []) as FGNode[]);
  const folders = $derived([...new Set(allNodes.map((n) => n.folder).filter(Boolean))].sort());
  const tags = $derived([...new Set(allNodes.flatMap((n) => n.tags))].sort());
  const sources = $derived([...new Set(allNodes.map((n) => n.source).filter(Boolean))].sort());
  const legend = $derived(legendEntries(allNodes, view.colorBy));
  const shapeLegend = $derived(shapeLegendEntries(allNodes, view.shapeBy));
  const selectedNode = $derived(allNodes.find((n) => n.id === view.selectedId) ?? null);
  // R is a plain (non-reactive) object read O(1) by the hot draw loop; the panel
  // reads this mirrored count instead so it stays reactive.
  let visibleCount = $state(0);

  // ---- filtering / scope ----
  function passes(n: FGNode): boolean {
    if (view.hideOrphans && n.deg === 0) return false;
    if (view.filterFolders.length && !view.filterFolders.includes(n.folder)) return false;
    if (view.filterSources.length && !view.filterSources.includes(n.source)) return false;
    if (view.filterTags.length && !n.tags.some((t) => view.filterTags.includes(t))) return false;
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
      nodes = nodes.filter((n) => within.has(n.id));
      links = links.filter((l) => within.has(linkEndId(l.source)) && within.has(linkEndId(l.target)));
    }
    return { nodes, links };
  }

  // recompute rebuilds the color + bright sets once per relevant change.
  function recompute() {
    const data = $graphQ.data;
    if (!data) return;
    const nodes = toForceGraph(data).nodes;
    const color = new Map<string, string>();
    const shape = new Map<string, ShapeKind>();
    for (const n of nodes) {
      color.set(n.id, colorOfNode(n, view.colorBy));
      shape.set(n.id, nodeShape(n, view.shapeBy));
    }

    const hoverSet =
      hoveredId && adjacency.has(hoveredId)
        ? new Set<string>([hoveredId, ...(adjacency.get(hoveredId) ?? [])])
        : null;

    const bright = new Set<string>();
    for (const n of nodes) {
      if (passes(n) && (!hoverSet || hoverSet.has(n.id))) bright.add(n.id);
    }
    R = { color, bright, shape };
    visibleCount = bright.size;
    graph?.refresh?.();
  }

  // ---- canvas helpers ----
  function nodeRadius(n: FGNode): number {
    return Math.min(12, Math.sqrt(1 + n.deg) * 3);
  }
  function dimmed(id: string): boolean {
    return !R.bright.has(id);
  }
  const LINK_BRIGHT = "rgba(128,128,128,0.5)";
  const LINK_DIM = "rgba(128,128,128,0.08)";
  const LINK_HOVER = "rgba(128,128,128,0.85)";

  function drawNode(n: FGNode, ctx: CanvasRenderingContext2D, scale: number) {
    const x = n.x ?? 0;
    const y = n.y ?? 0;
    const r = nodeRadius(n);
    const base = R.color.get(n.id) ?? cssAccent;
    const dim = dimmed(n.id);

    // root / selected ring
    if (n.id === view.rootId || n.id === view.selectedId) {
      ctx.beginPath();
      ctx.arc(x, y, r + 3, 0, 2 * Math.PI);
      ctx.strokeStyle = cssAccent;
      ctx.lineWidth = 1.5 / scale;
      ctx.stroke();
    }
    // node — shape encodes view.shapeBy (source/folder/tag); circle by default
    tracePath(ctx, R.shape.get(n.id) ?? "circle", x, y, r);
    ctx.fillStyle = dim ? base + "26" : base;
    ctx.fill();
    // orphans (no links) read as a hollow outline so they stand out
    if (n.deg === 0) {
      ctx.lineWidth = 1 / scale;
      ctx.strokeStyle = dim ? base + "55" : base;
      ctx.stroke();
    }
    // pinned indicator
    if (pinned.has(n.id)) {
      ctx.beginPath();
      ctx.arc(x, y, r + 1.5, 0, 2 * Math.PI);
      ctx.strokeStyle = dim ? cssFg + "55" : cssFg;
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
      // halo for legibility over links/nodes
      ctx.lineWidth = 3 / scale;
      ctx.strokeStyle = cssBg;
      ctx.strokeText(n.title, x, ty);
      ctx.fillStyle = cssFg;
      ctx.fillText(n.title, x, ty);
    }
  }

  // ---- selection / navigation ----
  function selectNode(n: FGNode) {
    view.selectedId = n.id;
    if (n.x != null && n.y != null) graph?.centerAt(n.x, n.y, reduce ? 0 : 450);
  }

  let clickTO: ReturnType<typeof setTimeout> | undefined;
  function onNodeClick(n: FGNode, evt: MouseEvent) {
    closeMenu();
    if (evt?.detail >= 2) {
      if (clickTO) clearTimeout(clickTO);
      navigate(n.url);
      return;
    }
    if (clickTO) clearTimeout(clickTO);
    clickTO = setTimeout(() => selectNode(n), 200);
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

  onMount(() => {
    let destroyed = false;
    let ro: ResizeObserver | undefined;
    let fitPending = false;
    readTheme();

    // re-fit whenever a fresh scoped dataset settles
    feedHook = () => {
      fitPending = true;
    };

    void (async () => {
      const { default: ForceGraph } = await import("force-graph");
      if (destroyed || !container) return;
      const g = new ForceGraph<FGNode, FGLink>(container)
        .nodeId("id")
        .nodeVal((n: FGNode) => 1 + n.deg)
        .nodeLabel((n: FGNode) => n.title)
        .backgroundColor(cssBg)
        // Painting happens in nodeCanvasObject (drawNode); force-graph ignores
        // nodeColor once a canvas-object accessor is set, so it's omitted here.
        .linkColor((l: any) => {
          const s = linkEndId(l.source);
          const t = linkEndId(l.target);
          if (hoveredId && (s === hoveredId || t === hoveredId)) return LINK_HOVER;
          return R.bright.has(s) && R.bright.has(t) ? LINK_BRIGHT : LINK_DIM;
        })
        .linkWidth((l: any) =>
          hoveredId && (linkEndId(l.source) === hoveredId || linkEndId(l.target) === hoveredId)
            ? 2
            : 1,
        )
        .nodeCanvasObject(drawNode)
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
        .onEngineStop(() => {
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

      graph = g;
      size();
      ro = new ResizeObserver(size);
      ro.observe(container);
    })();

    return () => {
      destroyed = true;
      if (clickTO) clearTimeout(clickTO);
      ro?.disconnect();
      graph?._destructor?.();
      graph = null;
    };
  });

  // hook the feed effect calls into onMount-scoped fitPending
  let feedHook: (() => void) | null = null;

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
    feedHook?.();
    graph.graphData(scopedData());
    // local mode roots a fresh layout — clear stale pins from the previous scope
    if (view.mode === "global") pinned = new Set();
  });

  // ---- recompute color/bright sets on filter/hover/colorBy change ----
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
      hoveredId,
      view.mode,
      view.depth,
      view.rootId,
    ];
    recompute();
  });

  // ---- physics ----
  $effect(() => {
    if (!graph) return;
    const charge = graph.d3Force("charge");
    if (charge?.strength) charge.strength(view.repel);
    const link = graph.d3Force("link");
    if (link?.distance) link.distance(view.linkDistance);
    graph.d3Force("x")?.strength(view.centerGravity);
    graph.d3Force("y")?.strength(view.centerGravity);
    if (!view.frozen) graph.d3ReheatSimulation();
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
      } else if (!pinned.has(n.id)) {
        n.fx = undefined;
        n.fy = undefined;
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
    graph?.refresh?.();
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
    ];
    savePersisted();
  });

  // ---- theme changes: refresh cached colors + canvas bg ----
  $effect(() => {
    if (typeof MutationObserver === "undefined") return;
    const obs = new MutationObserver(() => {
      readTheme();
      graph?.backgroundColor(cssBg);
      graph?.refresh?.();
    });
    obs.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme", "class"] });
    const mq = window.matchMedia?.("(prefers-color-scheme: dark)");
    const onMq = () => {
      readTheme();
      graph?.backgroundColor(cssBg);
      graph?.refresh?.();
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
    <div class="graph-overlay"><p class="muted">Loading graph…</p></div>
  {:else if $graphQ.error}
    <div class="graph-overlay"><p class="error">Couldn't load the graph.</p></div>
  {:else if allNodes.length === 0}
    <div class="graph-overlay">
      <div class="graph-empty">
        <h2>No graph yet</h2>
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
      nodeCount={allNodes.length}
      linkCount={$graphQ.data?.links.length ?? 0}
      {visibleCount}
      onFit={fit}
    />

    {#if allNodes.length > 1500 && view.mode === "global"}
      <div class="graph-banner">
        Large graph ({allNodes.length} notes). Select a node and switch to <strong>Local</strong> for a faster, clearer view.
      </div>
    {/if}

    {#if selectedNode}
      <GraphDetails
        node={selectedNode}
        pinned={pinned.has(selectedNode.id)}
        onOpen={() => selectedNode && navigate(selectedNode.url)}
        onFocusLocal={() => selectedNode && enterLocal(selectedNode.id)}
        onTogglePin={() => selectedNode && togglePin(selectedNode.id)}
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
  .graph-empty {
    text-align: center;
    max-width: 340px;
  }
  .graph-empty h2 {
    margin: 0 0 8px;
  }
  .graph-banner {
    position: absolute;
    top: 12px;
    left: 50%;
    transform: translateX(-50%);
    z-index: 15;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 7px 14px;
    font-size: 0.8rem;
    color: var(--fg-soft);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.18);
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
