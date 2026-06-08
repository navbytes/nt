<script lang="ts">
  import { onMount } from "svelte";
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import { navigate } from "../lib/router.svelte";
  import { toForceGraph, type FGNode, type FGLink } from "../lib/graph";

  const graphQ = createQuery({ queryKey: ["graph"], queryFn: api.graph });

  let container: HTMLDivElement | undefined = $state();
  let graph = $state<any>(null);
  let searchText = $state("");
  let filterFolder = $state("");

  // Folder → deterministic color from Tokyo Night accent palette.
  const PALETTE = [
    "#7aa2f7", "#9ece6a", "#ff9e64", "#f7768e",
    "#7dcfff", "#bb9af7", "#e0af68", "#2ac3de",
    "#73daca", "#b4f9f8",
  ];
  const folderColors = new Map<string, string>();
  function colorFor(folder: string): string {
    if (!folder) return "#87837a";
    if (!folderColors.has(folder)) {
      folderColors.set(folder, PALETTE[folderColors.size % PALETTE.length] as string);
    }
    return folderColors.get(folder)!;
  }

  // Derive folders for the legend / filter dropdown.
  const folders = $derived(
    [...new Set(($graphQ.data?.nodes ?? []).map((n) => n.folder ?? "").filter(Boolean))].sort(),
  );

  // Filter visible nodes.
  function visibleNodes(nodes: FGNode[]): Set<string> {
    const q = searchText.toLowerCase().trim();
    return new Set(
      nodes
        .filter((n) => {
          if (filterFolder && n.folder !== filterFolder) return false;
          if (q && !n.title.toLowerCase().includes(q)) return false;
          return true;
        })
        .map((n) => n.id),
    );
  }

  let hoveredId = $state<string | null>(null);

  function isDim(nodeId: string, visibles: Set<string>): boolean {
    if (!visibles.has(nodeId)) return true;
    if (!hoveredId) return false;
    if (nodeId === hoveredId) return false;
    // Keep neighbors of hovered node bright.
    const data = $graphQ.data;
    if (!data) return false;
    for (const l of data.links) {
      const s = data.nodes[l.s];
      const t = data.nodes[l.t];
      if (s && t && (s.id === hoveredId || t.id === hoveredId) && (s.id === nodeId || t.id === nodeId))
        return false;
    }
    return true;
  }

  function getCSSVar(name: string): string {
    return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  }

  function getThemeBg(): string {
    return getCSSVar("--bg") || "rgba(0,0,0,0)";
  }

  function buildGraph(el: HTMLDivElement) {
    let destroyed = false;
    let ro: ResizeObserver | undefined;

    void (async () => {
      const { default: ForceGraph } = await import("force-graph");
      if (destroyed || !el) return;

      const bg = getThemeBg();
      graph = new ForceGraph<FGNode, FGLink>(el)
        .nodeId("id")
        .nodeLabel("title")
        .nodeVal((n: FGNode) => 1 + n.deg)
        .nodeColor((n: FGNode) => {
          const data = $graphQ.data;
          if (!data) return colorFor(n.folder);
          const vis = visibleNodes(toForceGraph(data).nodes);
          const dim = isDim(n.id, vis);
          const base = colorFor(n.folder);
          return dim ? base + "33" : base;
        })
        .linkColor((l: any) => {
          const src = typeof l.source === "object" ? l.source.id : l.source;
          const tgt = typeof l.target === "object" ? l.target.id : l.target;
          const data = $graphQ.data;
          if (!data) return "rgba(128,128,128,0.25)";
          const vis = visibleNodes(toForceGraph(data).nodes);
          if (!vis.has(src) || !vis.has(tgt)) return "rgba(128,128,128,0.06)";
          if (hoveredId && src !== hoveredId && tgt !== hoveredId) return "rgba(128,128,128,0.12)";
          return "rgba(128,128,128,0.5)";
        })
        .linkWidth((l: any) => {
          const src = typeof l.source === "object" ? l.source.id : l.source;
          const tgt = typeof l.target === "object" ? l.target.id : l.target;
          if (hoveredId && (src === hoveredId || tgt === hoveredId)) return 2;
          return 1;
        })
        .backgroundColor(bg)
        .nodeCanvasObject((n: FGNode, ctx: CanvasRenderingContext2D, scale: number) => {
          const r = Math.sqrt(1 + n.deg) * 3;
          const data = $graphQ.data;
          const vis = data ? visibleNodes(toForceGraph(data).nodes) : new Set([n.id]);
          const dim = isDim(n.id, vis);
          const base = colorFor(n.folder);
          ctx.beginPath();
          ctx.arc((n as any).x, (n as any).y, r, 0, 2 * Math.PI);
          ctx.fillStyle = dim ? base + "33" : base;
          ctx.fill();
          // Draw label past a zoom threshold or for the hovered / high-degree node.
          if (scale > 1.5 || n.id === hoveredId || n.deg >= 4) {
            ctx.font = `${Math.min(5, 14 / scale)}px sans-serif`;
            ctx.fillStyle = dim ? "#88888844" : getCSSVar("--fg") || "#ccc";
            ctx.textAlign = "center";
            ctx.fillText(n.title, (n as any).x, (n as any).y + r + 4);
          }
        })
        .nodePointerAreaPaint((n: FGNode, color: string, ctx: CanvasRenderingContext2D) => {
          const r = Math.sqrt(1 + n.deg) * 3 + 2;
          ctx.beginPath();
          ctx.arc((n as any).x, (n as any).y, r, 0, 2 * Math.PI);
          ctx.fillStyle = color;
          ctx.fill();
        })
        .onNodeHover((n: FGNode | null) => {
          hoveredId = n?.id ?? null;
          graph?.refresh();
        })
        .onNodeClick((n: FGNode) => {
          if (n.url) navigate(n.url);
        })
        .onEngineStop(() => {
          graph?.zoomToFit(400, 40);
        });

      size();
      ro = new ResizeObserver(size);
      ro.observe(el);
    })();

    return () => {
      destroyed = true;
      ro?.disconnect();
      graph?._destructor?.();
      graph = null;
    };
  }

  function size() {
    if (graph && container) graph.width(container.clientWidth).height(container.clientHeight);
  }

  onMount(() => {
    if (!container) return;
    return buildGraph(container);
  });

  // Feed data once both the renderer and the query are ready.
  $effect(() => {
    const data = $graphQ.data;
    if (graph && data) graph.graphData(toForceGraph(data));
  });

  // Re-render on filter/search changes.
  $effect(() => {
    if (!graph) return;
    void searchText; void filterFolder; void hoveredId;
    graph?.refresh();
  });

  function zoomToNode(title: string) {
    const data = $graphQ.data;
    if (!data || !graph) return;
    const hit = toForceGraph(data).nodes.find((n) => n.title.toLowerCase() === title.toLowerCase());
    if (hit) graph.centerAt((hit as any).x, (hit as any).y, 600);
  }
</script>

<div class="pagehead">
  <h1>Graph</h1>
  {#if $graphQ.data}
    <span class="muted small">{$graphQ.data.nodes.length} notes · {$graphQ.data.links.length} links</span>
  {/if}
</div>

<div class="graph-toolbar">
  <input
    class="graph-search"
    type="search"
    placeholder="Find node…"
    bind:value={searchText}
    onkeydown={(e) => e.key === "Enter" && zoomToNode(searchText)}
  />
  <select class="graph-filter" bind:value={filterFolder}>
    <option value="">All folders</option>
    {#each folders as f (f)}
      <option value={f}>{f || "(root)"}</option>
    {/each}
  </select>
  {#if filterFolder || searchText}
    <button class="btn btn--ghost btn--sm" onclick={() => { filterFolder = ""; searchText = ""; }}>Clear</button>
  {/if}
</div>

{#if $graphQ.data && folders.length > 1}
  <div class="graph-legend">
    {#each folders as f (f)}
      <span class="legend-item">
        <span class="legend-dot" style="background:{colorFor(f)}"></span>{f || "(root)"}
      </span>
    {/each}
  </div>
{/if}

{#if $graphQ.error}
  <p class="error">Couldn't load the graph.</p>
{/if}

<div class="graph" bind:this={container}></div>

<style>
  .graph-toolbar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 0 10px;
    flex-wrap: wrap;
  }
  .graph-search {
    padding: 4px 8px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--bg-inset);
    color: var(--fg);
    font-size: 0.85rem;
    width: 180px;
  }
  .graph-filter {
    padding: 4px 8px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--bg-inset);
    color: var(--fg);
    font-size: 0.85rem;
  }
  .graph-legend {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
    padding: 0 0 10px;
    font-size: 0.78rem;
    color: var(--muted);
  }
  .legend-item {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .legend-dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    display: inline-block;
  }
</style>
