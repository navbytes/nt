<script lang="ts">
  import {
    view,
    toggleIn,
    resetFilters,
    exitLocal,
    type ColorBy,
    type ShapeBy,
    type SizeBy,
    type Effects,
  } from "./graphView.svelte";
  import ShapeGlyph from "./ShapeGlyph.svelte";
  import type { ShapeKind } from "./graphShapes";

  let {
    folders,
    tags,
    sources,
    legend,
    shapeLegend,
    edgeLegend,
    nodeCount,
    linkCount,
    visibleCount,
    onFit,
  }: {
    folders: string[];
    tags: string[];
    sources: string[];
    legend: { value: string; label: string; color: string }[];
    shapeLegend: { value: string; label: string; shape: ShapeKind }[];
    edgeLegend: { value: string; label: string; color: string }[];
    nodeCount: number;
    linkCount: number;
    visibleCount: number;
    onFit: () => void;
  } = $props();

  let open = $state(window.innerWidth >= 720);
  let advanced = $state(false);

  const colorOptions: { v: ColorBy; label: string }[] = [
    { v: "folder", label: "Folder" },
    { v: "tag", label: "Tag" },
    { v: "source", label: "Source" },
    { v: "none", label: "None" },
  ];

  const shapeOptions: { v: ShapeBy; label: string }[] = [
    { v: "kind", label: "Kind (note/task)" },
    { v: "source", label: "Source" },
    { v: "folder", label: "Folder" },
    { v: "tag", label: "Tag" },
    { v: "none", label: "None" },
  ];

  const sizeOptions: { v: SizeBy; label: string }[] = [
    { v: "degree", label: "Degree (link count)" },
    { v: "centrality", label: "Centrality (PageRank)" },
  ];

  const effectsOptions: { v: Effects; label: string }[] = [
    { v: "full", label: "Full (glow + animation)" },
    { v: "subtle", label: "Subtle (glow)" },
    { v: "off", label: "Off (flat)" },
  ];

  const hasFilters = $derived(
    view.search !== "" ||
      view.filterFolders.length > 0 ||
      view.filterTags.length > 0 ||
      view.filterSources.length > 0 ||
      view.hideOrphans,
  );

  // Clicking a legend swatch toggles a filter on that dimension's value.
  function legendClick(value: string) {
    if (view.colorBy === "folder") view.filterFolders = toggleIn(view.filterFolders, value);
    else if (view.colorBy === "tag") view.filterTags = toggleIn(view.filterTags, value);
    else if (view.colorBy === "source") view.filterSources = toggleIn(view.filterSources, value);
  }

  function legendActive(value: string): boolean {
    if (view.colorBy === "folder") return view.filterFolders.includes(value);
    if (view.colorBy === "tag") return view.filterTags.includes(value);
    if (view.colorBy === "source") return view.filterSources.includes(value);
    return false;
  }
</script>

<div class="gctl" class:gctl--open={open}>
  <div class="gctl__bar">
    <button class="gctl__toggle" onclick={() => (open = !open)} aria-expanded={open} title="Toggle controls">
      <span class="gctl__chev">{open ? "▾" : "▸"}</span> Graph
    </button>
    <button class="icon-btn" title="Fit to view (f)" aria-label="Fit graph to view" onclick={onFit}>⛶</button>
  </div>

  {#if open}
    <div class="gctl__body">
      <!-- Mode -->
      <div class="gctl__row">
        <div class="seg" role="group" aria-label="Graph mode">
          <button class:seg--on={view.mode === "global"} onclick={exitLocal}>Global</button>
          <button class:seg--on={view.mode === "local"} disabled={!view.rootId && !view.selectedId}
            onclick={() => { view.mode = "local"; view.rootId = view.rootId ?? view.selectedId; }}>Local</button>
        </div>
      </div>

      <!-- Renderer: 2D canvas / 3D WebGL constellation -->
      <div class="gctl__row">
        <div class="seg" role="group" aria-label="Renderer dimension">
          <button class:seg--on={view.dim === "2d"} onclick={() => (view.dim = "2d")}>2D</button>
          <button class:seg--on={view.dim === "3d"} onclick={() => (view.dim = "3d")} title="3D constellation with bloom">3D ✦</button>
        </div>
      </div>
      <!-- Layout: organic force vs. radial ego rings (radial needs a local root) -->
      <div class="gctl__row">
        <div class="seg" role="group" aria-label="Layout">
          <button class:seg--on={view.layout === "force"} onclick={() => (view.layout = "force")}>Force</button>
          <button class:seg--on={view.layout === "radial"} onclick={() => (view.layout = "radial")}
            title="Concentric rings by distance from the focused node (Local mode, 2D)">Radial</button>
        </div>
      </div>
      {#if view.layout === "radial" && !(view.mode === "local" && view.dim === "2d")}
        <p class="gctl__hint">Radial applies in <strong>Local</strong> mode on the <strong>2D</strong> renderer.</p>
      {/if}
      {#if view.mode === "local"}
        <div class="gctl__row gctl__depth">
          <label for="g-depth">Depth {view.depth}</label>
          <input id="g-depth" type="range" min="1" max="5" step="1" bind:value={view.depth} />
        </div>
        <p class="gctl__hint">Shift-click a node to reveal its neighbors.</p>
      {/if}

      <!-- Search -->
      <div class="gctl__row">
        <input
          id="graph-search"
          class="gctl__search"
          type="search"
          placeholder="Search nodes  (/)"
          bind:value={view.search}
        />
      </div>

      <!-- Color by -->
      <div class="gctl__row gctl__field">
        <label for="g-colorby">Color by</label>
        <select id="g-colorby" bind:value={view.colorBy}>
          {#each colorOptions as o (o.v)}<option value={o.v}>{o.label}</option>{/each}
        </select>
      </div>

      <!-- Shape by -->
      <div class="gctl__row gctl__field">
        <label for="g-shapeby">Shape by</label>
        <select id="g-shapeby" bind:value={view.shapeBy}>
          {#each shapeOptions as o (o.v)}<option value={o.v}>{o.label}</option>{/each}
        </select>
      </div>

      <!-- Size by -->
      <div class="gctl__row gctl__field">
        <label for="g-sizeby">Size by</label>
        <select id="g-sizeby" bind:value={view.sizeBy}>
          {#each sizeOptions as o (o.v)}<option value={o.v}>{o.label}</option>{/each}
        </select>
      </div>

      <!-- Filters -->
      {#if sources.length > 1}
        <div class="gctl__chips" aria-label="Filter by source">
          {#each sources as s (s)}
            <button class="chip-toggle" class:on={view.filterSources.includes(s)}
              onclick={() => (view.filterSources = toggleIn(view.filterSources, s))}>{s}</button>
          {/each}
        </div>
      {/if}
      {#if folders.length > 1 || tags.length > 0}
        <div class="gctl__filtersel">
          {#if folders.length > 1}
            <select aria-label="Filter by folder" onchange={(e) => {
              const v = (e.currentTarget as HTMLSelectElement).value;
              if (v) view.filterFolders = toggleIn(view.filterFolders, v);
              (e.currentTarget as HTMLSelectElement).value = "";
            }}>
              <option value="">+ folder…</option>
              {#each folders as f (f)}<option value={f}>{f}</option>{/each}
            </select>
          {/if}
          {#if tags.length > 0}
            <select aria-label="Filter by tag" onchange={(e) => {
              const v = (e.currentTarget as HTMLSelectElement).value;
              if (v) view.filterTags = toggleIn(view.filterTags, v);
              (e.currentTarget as HTMLSelectElement).value = "";
            }}>
              <option value="">+ tag…</option>
              {#each tags as t (t)}<option value={t}>#{t}</option>{/each}
            </select>
          {/if}
        </div>
      {/if}
      {#if view.filterFolders.length || view.filterTags.length}
        <div class="gctl__chips">
          {#each view.filterFolders as f (f)}
            <button class="chip-toggle on" onclick={() => (view.filterFolders = toggleIn(view.filterFolders, f))}>{f} ×</button>
          {/each}
          {#each view.filterTags as t (t)}
            <button class="chip-toggle on" onclick={() => (view.filterTags = toggleIn(view.filterTags, t))}>#{t} ×</button>
          {/each}
        </div>
      {/if}

      <div class="gctl__row gctl__inline">
        <label><input type="checkbox" bind:checked={view.showTasks} /> Show tasks</label>
        <label><input type="checkbox" bind:checked={view.hideOrphans} /> Hide orphans</label>
        {#if hasFilters}<button class="linklike" onclick={resetFilters}>Clear filters</button>{/if}
      </div>

      <!-- Advanced -->
      <button class="gctl__adv" onclick={() => (advanced = !advanced)} aria-expanded={advanced}>
        {advanced ? "▾" : "▸"} Physics &amp; display
      </button>
      {#if advanced}
        <div class="gctl__adv-body">
          <div class="gctl__inline">
            <label><input type="checkbox" bind:checked={view.showLabels} /> Labels</label>
            <label><input type="checkbox" bind:checked={view.showArrows} /> Arrows</label>
            <label><input type="checkbox" bind:checked={view.particles} /> Flow</label>
          </div>
          <div class="gctl__field">
            <label for="g-effects">Effects</label>
            <select id="g-effects" bind:value={view.effects}>
              {#each effectsOptions as o (o.v)}<option value={o.v}>{o.label}</option>{/each}
            </select>
          </div>
          <div class="gctl__inline">
            <label><input type="checkbox" bind:checked={view.colorLinks} /> Color links</label>
            <label>
              <input
                type="checkbox"
                checked={view.linkStyle === "curved"}
                onchange={(e) => (view.linkStyle = e.currentTarget.checked ? "curved" : "straight")}
              /> Curved links
            </label>
          </div>
          <label
            title="Color each edge by its relationship: wikilink, task reference, or dependency"
          >
            <input type="checkbox" bind:checked={view.colorLinksByType} /> Color edges by type
          </label>
          <label><input type="checkbox" bind:checked={view.frozen} /> Freeze layout</label>
          {#if view.dim === "3d"}
            <label title="Slowly orbit the 3D constellation">
              <input type="checkbox" bind:checked={view.autoRotate} /> Auto-rotate
            </label>
          {/if}
          <div class="gctl__slider">
            <label for="g-repel">Repel</label>
            <input id="g-repel" type="range" min="-400" max="-20" step="10" bind:value={view.repel} />
          </div>
          <div class="gctl__slider">
            <label for="g-linkd">Link distance</label>
            <input id="g-linkd" type="range" min="10" max="120" step="5" bind:value={view.linkDistance} />
          </div>
          <div class="gctl__slider">
            <label for="g-grav">Center gravity</label>
            <input id="g-grav" type="range" min="0" max="0.5" step="0.02" bind:value={view.centerGravity} />
          </div>
          <div class="gctl__slider">
            <label for="g-label">Label zoom</label>
            <input id="g-label" type="range" min="0.4" max="3" step="0.1" bind:value={view.labelThreshold} />
          </div>
        </div>
      {/if}

      <!-- Legend -->
      {#if legend.length > 1}
        <div class="gctl__legend" aria-label="Color legend (click to filter)">
          {#each legend as e (e.value)}
            <button class="legend-item" class:on={legendActive(e.value)}
              disabled={view.colorBy === "none"} onclick={() => legendClick(e.value)}>
              <span class="legend-dot" style="background:{e.color}"></span>{e.label}
            </button>
          {/each}
        </div>
      {/if}

      <!-- Shape legend (what the node shapes mean) -->
      {#if shapeLegend.length > 1}
        <div class="gctl__legend gctl__shapelegend" aria-label="Shape legend">
          {#each shapeLegend as e (e.value)}
            <span class="shape-legend-item"><ShapeGlyph kind={e.shape} /> {e.label}</span>
          {/each}
        </div>
      {/if}

      <!-- Edge legend (relationship colors) — only when coloring edges by type -->
      {#if view.colorLinksByType && edgeLegend.length}
        <div class="gctl__legend" aria-label="Edge type legend">
          {#each edgeLegend as e (e.value)}
            <span class="legend-item">
              <span class="legend-line" style="background:{e.color}"></span>{e.label}
            </span>
          {/each}
        </div>
      {/if}

      <div class="gctl__stats">
        {visibleCount}/{nodeCount} notes · {linkCount} links{#if view.mode === "local"} · local depth {view.depth}{/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .gctl {
    position: absolute;
    top: 12px;
    left: 12px;
    z-index: 20;
    width: 232px;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    box-shadow: 0 4px 18px rgba(0, 0, 0, 0.16);
    font-size: 0.82rem;
  }
  .gctl__bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 8px 6px 4px;
  }
  .gctl__toggle {
    background: none;
    border: none;
    color: var(--fg);
    font-weight: 600;
    cursor: pointer;
    font-size: 0.85rem;
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .gctl__chev {
    color: var(--muted);
    font-size: 0.7rem;
  }
  .gctl__body {
    padding: 4px 10px 10px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-height: calc(100vh - 140px);
    overflow-y: auto;
  }
  .gctl__row {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .gctl__field,
  .gctl__depth {
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  .gctl__depth input,
  .gctl__slider input {
    flex: 1;
  }
  .gctl__inline {
    display: flex;
    flex-direction: row;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
  }
  .gctl__search,
  .gctl select {
    width: 100%;
    padding: 5px 8px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--bg-inset);
    color: var(--fg);
    font-size: 0.82rem;
  }
  .gctl__field select {
    width: auto;
  }
  .seg {
    display: flex;
    width: 100%;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }
  .seg button {
    flex: 1;
    padding: 5px 6px;
    background: var(--bg-inset);
    border: none;
    color: var(--fg-soft);
    cursor: pointer;
    font-size: 0.8rem;
  }
  .seg button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .seg--on {
    background: var(--accent) !important;
    color: #fff !important;
  }
  .gctl__chips,
  .gctl__filtersel {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
  }
  .gctl__filtersel select {
    width: auto;
    flex: 1;
    min-width: 90px;
  }
  .chip-toggle {
    padding: 3px 8px;
    border: 1px solid var(--border);
    border-radius: 999px;
    background: var(--bg-inset);
    color: var(--fg-soft);
    cursor: pointer;
    font-size: 0.75rem;
  }
  .chip-toggle.on {
    background: var(--accent);
    border-color: var(--accent);
    color: #fff;
  }
  .gctl__adv {
    background: none;
    border: none;
    color: var(--muted);
    cursor: pointer;
    text-align: left;
    padding: 2px 0;
    font-size: 0.8rem;
  }
  .gctl__adv-body {
    display: flex;
    flex-direction: column;
    gap: 7px;
    padding: 2px 0 4px;
    border-left: 2px solid var(--border-soft);
    padding-left: 8px;
  }
  .gctl__slider {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .gctl__slider label {
    min-width: 78px;
    color: var(--muted);
    font-size: 0.75rem;
  }
  .gctl__legend {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 8px;
    border-top: 1px solid var(--border-soft);
    padding-top: 8px;
  }
  .legend-item {
    display: flex;
    align-items: center;
    gap: 4px;
    background: none;
    border: none;
    color: var(--muted);
    cursor: pointer;
    font-size: 0.75rem;
    padding: 1px 3px;
    border-radius: var(--radius-sm);
  }
  .legend-item.on {
    color: var(--fg);
    background: var(--bg-inset);
  }
  .legend-item:disabled {
    cursor: default;
  }
  .legend-dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    display: inline-block;
    flex: none;
  }
  .legend-line {
    width: 14px;
    height: 3px;
    border-radius: 2px;
    display: inline-block;
    flex: none;
  }
  .gctl__hint {
    margin: 0;
    color: var(--muted);
    font-size: 0.72rem;
    line-height: 1.35;
  }
  .shape-legend-item {
    display: flex;
    align-items: center;
    gap: 4px;
    color: var(--fg-soft);
    font-size: 0.75rem;
    padding: 1px 3px;
  }
  .gctl__stats {
    color: var(--muted);
    font-size: 0.73rem;
    border-top: 1px solid var(--border-soft);
    padding-top: 6px;
  }
  .linklike {
    background: none;
    border: none;
    color: var(--accent);
    cursor: pointer;
    font-size: 0.78rem;
    padding: 0;
  }
</style>
