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
  import Icon from "./Icon.svelte";
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
      <span class="gctl__chev" class:gctl__chev--open={open}><Icon name="chevron-right" size={14} /></span>
      <span class="gctl__brand">Graph</span>
    </button>
    <button class="icon-btn gctl__fit" title="Fit to view (f)" aria-label="Fit graph to view" onclick={onFit}><Icon name="focus" /></button>
  </div>

  {#if open}
    <div class="gctl__body">
      <!-- Mode -->
      <div class="gctl__row">
        <div class="seg" role="group" aria-label="Graph mode">
          <button class:seg--on={view.mode === "global"} aria-pressed={view.mode === "global"} onclick={exitLocal}>Global</button>
          <button class:seg--on={view.mode === "local"} aria-pressed={view.mode === "local"} disabled={!view.rootId && !view.selectedId}
            onclick={() => { view.mode = "local"; view.rootId = view.rootId ?? view.selectedId; }}>Local</button>
        </div>
      </div>

      <!-- Renderer: 2D canvas / 3D WebGL constellation -->
      <div class="gctl__row">
        <div class="seg" role="group" aria-label="Renderer dimension">
          <button class:seg--on={view.dim === "2d"} aria-pressed={view.dim === "2d"} onclick={() => (view.dim = "2d")}>2D</button>
          <button class:seg--on={view.dim === "3d"} aria-pressed={view.dim === "3d"} onclick={() => (view.dim = "3d")} title="3D constellation with bloom">3D ✦</button>
        </div>
      </div>
      <!-- Layout: organic force vs. radial ego rings (radial needs a local root) -->
      <div class="gctl__row">
        <div class="seg" role="group" aria-label="Layout">
          <button class:seg--on={view.layout === "force"} aria-pressed={view.layout === "force"} onclick={() => (view.layout = "force")}>Force</button>
          <button class:seg--on={view.layout === "radial"} aria-pressed={view.layout === "radial"} onclick={() => (view.layout = "radial")}
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
      <div class="gctl__row gctl__search-row">
        <span class="gctl__search-ico" aria-hidden="true"><Icon name="search" size={14} /></span>
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
            <button class="chip-toggle on" aria-label={`Remove folder filter ${f}`} onclick={() => (view.filterFolders = toggleIn(view.filterFolders, f))}>{f} <Icon name="close" size={11} /></button>
          {/each}
          {#each view.filterTags as t (t)}
            <button class="chip-toggle on" aria-label={`Remove tag filter ${t}`} onclick={() => (view.filterTags = toggleIn(view.filterTags, t))}>#{t} <Icon name="close" size={11} /></button>
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
        <span class="gctl__chev" class:gctl__chev--open={advanced}><Icon name="chevron-right" size={13} /></span> Physics &amp; display
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
        <div class="gctl__legendgroup">
          <p class="gctl__legendhead">{view.colorBy}</p>
          <div class="gctl__legend" aria-label="Color legend (click to filter)">
            {#each legend as e (e.value)}
              <button class="legend-item" class:on={legendActive(e.value)}
                disabled={view.colorBy === "none"} onclick={() => legendClick(e.value)}>
                <span class="legend-dot" style="background:{e.color}"></span>{e.label}
              </button>
            {/each}
          </div>
        </div>
      {/if}

      <!-- Shape legend (what the node shapes mean) -->
      {#if shapeLegend.length > 1}
        <div class="gctl__legendgroup">
          <p class="gctl__legendhead">Shapes</p>
          <div class="gctl__legend gctl__shapelegend" aria-label="Shape legend">
            {#each shapeLegend as e (e.value)}
              <span class="shape-legend-item"><ShapeGlyph kind={e.shape} /> {e.label}</span>
            {/each}
          </div>
        </div>
      {/if}

      <!-- Edge legend (relationship colors) — only when coloring edges by type -->
      {#if view.colorLinksByType && edgeLegend.length}
        <div class="gctl__legendgroup">
          <p class="gctl__legendhead">Edges</p>
          <div class="gctl__legend" aria-label="Edge type legend">
            {#each edgeLegend as e (e.value)}
              <span class="legend-item">
                <span class="legend-line" style="background:{e.color}"></span>{e.label}
              </span>
            {/each}
          </div>
        </div>
      {/if}

      <div class="gctl__stats">
        <span class="gctl__stat"><strong>{visibleCount}</strong>/{nodeCount} notes</span>
        <span class="gctl__stat-sep">·</span>
        <span class="gctl__stat"><strong>{linkCount}</strong> links</span>
        {#if view.mode === "local"}<span class="gctl__stat-sep">·</span><span class="gctl__stat">depth {view.depth}</span>{/if}
      </div>
    </div>
  {/if}
</div>

<style>
  /* Glass floating control panel — the graph's "cockpit". Translucent over the
     canvas with the spatial float shadow + top light-catch hairline, and a
     spectral hairline along the very top edge for brand identity. */
  .gctl {
    position: absolute;
    top: 12px;
    left: 12px;
    z-index: 20;
    width: 236px;
    background: color-mix(in srgb, var(--bg-elevated) 86%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    border: 0.5px solid var(--separator);
    border-radius: var(--radius-popover);
    box-shadow: var(--shadow-float), var(--glass-hairline);
    font-size: var(--text-body);
    overflow: hidden;
  }
  .gctl::before {
    content: "";
    position: absolute;
    inset: 0 0 auto 0;
    height: 2px;
    background: var(--grad-spectral);
    opacity: 0.85;
    pointer-events: none;
  }
  .gctl__bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-2) var(--space-2) var(--space-2) var(--space-1);
  }
  .gctl__toggle {
    background: none;
    border: none;
    color: var(--fg);
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: var(--space-1);
    padding: 4px;
  }
  /* mono wordmark — echoes the sidebar brand + the cockpit microlabel voice */
  .gctl__brand {
    font-family: var(--font-mono);
    font-size: var(--text-callout);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
  }
  .gctl__fit {
    min-width: 26px;
    min-height: 26px;
    border-color: transparent;
    background: transparent;
  }
  /* Disclosure chevron: points right when closed, rotates down when open
     (the global reduced-motion rule neutralizes the spin). */
  .gctl__chev {
    display: inline-flex;
    color: var(--muted);
    transition: transform var(--motion-fast) var(--ease);
  }
  .gctl__chev--open {
    transform: rotate(90deg);
  }
  .gctl__body {
    padding: var(--space-1) var(--space-4) var(--space-4);
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    max-height: calc(100vh - 140px);
    overflow-y: auto;
  }
  .gctl__row {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .gctl__field,
  .gctl__depth {
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
  }
  .gctl__field label,
  .gctl__depth label {
    color: var(--label-secondary);
  }
  .gctl__depth input,
  .gctl__slider input {
    flex: 1;
  }
  .gctl__inline {
    display: flex;
    flex-direction: row;
    align-items: center;
    gap: var(--space-4);
    flex-wrap: wrap;
    color: var(--label-secondary);
  }
  /* Search field with a leading glyph (NSSearchField feel). */
  .gctl__search-row {
    position: relative;
    flex-direction: row;
    align-items: center;
  }
  .gctl__search-ico {
    position: absolute;
    left: 8px;
    display: inline-flex;
    color: var(--muted);
    pointer-events: none;
  }
  .gctl__search-row .gctl__search {
    padding-left: 28px;
  }
  .gctl__search,
  .gctl select {
    width: 100%;
    padding: 5px 8px;
    border: 0.5px solid var(--separator-strong);
    border-radius: var(--radius-sm);
    background: var(--fill);
    color: var(--fg);
    font-size: var(--text-body);
    transition:
      border-color var(--motion-fast) var(--ease),
      box-shadow var(--motion-fast) var(--ease);
  }
  .gctl__field select {
    width: auto;
  }
  /* macOS segmented control: a gray hairline track holds equal segments; the
     selected one lifts onto a spectral-tinted pill (NSSegmentedControl). */
  .seg {
    display: flex;
    width: 100%;
    gap: 2px;
    padding: 2px;
    background: var(--fill);
    border: 0.5px solid var(--separator);
    border-radius: var(--radius-sm);
  }
  .seg button {
    flex: 1;
    padding: 4px 6px;
    background: transparent;
    border: 0.5px solid transparent;
    border-radius: var(--radius-xs);
    color: var(--fg-soft);
    cursor: pointer;
    font-size: var(--text-callout);
    transition:
      background var(--motion-fast) var(--ease),
      color var(--motion-fast) var(--ease),
      box-shadow var(--motion-fast) var(--ease);
  }
  .seg button:hover:not(.seg--on):not(:disabled) {
    color: var(--fg);
  }
  .seg button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  /* active segment — elevated pill washed with a soft spectral tint + accent text */
  .seg--on {
    background:
      linear-gradient(
        135deg,
        color-mix(in srgb, var(--spectral-1) 22%, var(--bg-elevated)),
        color-mix(in srgb, var(--spectral-3) 22%, var(--bg-elevated))
      );
    border-color: color-mix(in srgb, var(--spectral-2) 45%, transparent);
    box-shadow: var(--shadow-control);
    color: var(--fg);
    font-weight: 600;
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
    display: inline-flex;
    align-items: center;
    gap: 3px;
    padding: 3px 8px;
    border: 0.5px solid var(--separator-strong);
    border-radius: 999px;
    background: var(--fill);
    color: var(--fg-soft);
    cursor: pointer;
    font-size: var(--text-callout);
    transition:
      background var(--motion-fast) var(--ease),
      color var(--motion-fast) var(--ease),
      border-color var(--motion-fast) var(--ease);
  }
  .chip-toggle:hover {
    color: var(--fg);
    border-color: var(--fg-soft);
  }
  /* active filter chip — spectral wash (calm; never carries the label colour) */
  .chip-toggle.on {
    background: color-mix(in srgb, var(--spectral-2) 18%, transparent);
    border-color: color-mix(in srgb, var(--spectral-2) 45%, transparent);
    color: var(--fg);
  }
  .chip-toggle.on:hover {
    color: var(--fg);
    border-color: color-mix(in srgb, var(--spectral-2) 70%, transparent);
  }
  .gctl__adv {
    background: none;
    border: none;
    color: var(--muted);
    cursor: pointer;
    text-align: left;
    padding: 2px 0;
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    display: flex;
    align-items: center;
    gap: var(--space-1);
  }
  .gctl__adv-body {
    display: flex;
    flex-direction: column;
    gap: 7px;
    padding: 2px 0 4px;
    border-left: 0.5px solid var(--separator);
    padding-left: var(--space-3);
    color: var(--label-secondary);
  }
  .gctl__slider {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }
  .gctl__slider label {
    min-width: 78px;
    color: var(--muted);
    font-size: var(--text-callout);
  }
  /* Each legend is a titled group: a mono microlabel header over the swatches. */
  .gctl__legendgroup {
    border-top: 0.5px solid var(--separator);
    padding-top: var(--space-3);
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .gctl__legendhead {
    margin: 0;
    font-family: var(--font-mono);
    font-size: var(--text-footnote);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    color: var(--muted);
  }
  .gctl__legend {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 8px;
  }
  .legend-item {
    display: flex;
    align-items: center;
    gap: 5px;
    background: none;
    border: 0.5px solid transparent;
    color: var(--label-secondary);
    cursor: pointer;
    font-size: var(--text-callout);
    padding: 2px 7px;
    border-radius: 999px;
    transition:
      background var(--motion-fast) var(--ease),
      color var(--motion-fast) var(--ease),
      border-color var(--motion-fast) var(--ease);
  }
  .legend-item:hover:not(:disabled) {
    color: var(--fg);
    background: var(--fill);
  }
  /* active legend filter — spectral ring, matching the filter chips */
  .legend-item.on {
    color: var(--fg);
    background: color-mix(in srgb, var(--spectral-2) 16%, transparent);
    border-color: color-mix(in srgb, var(--spectral-2) 42%, transparent);
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
    box-shadow: 0 0 0 0.5px rgba(0, 0, 0, 0.15);
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
    font-size: var(--text-callout);
    line-height: 1.35;
  }
  .shape-legend-item {
    display: flex;
    align-items: center;
    gap: 5px;
    color: var(--label-secondary);
    font-size: var(--text-callout);
    padding: 2px 3px;
  }
  .gctl__stats {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 5px;
    color: var(--muted);
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    border-top: 0.5px solid var(--separator);
    padding-top: var(--space-2);
    font-variant-numeric: tabular-nums;
  }
  .gctl__stat strong {
    color: var(--label-secondary);
    font-weight: 600;
  }
  .gctl__stat-sep {
    color: var(--separator-strong);
  }
  .linklike {
    background: none;
    border: none;
    color: var(--accent-color);
    cursor: pointer;
    font-size: var(--text-callout);
    padding: 0;
  }
</style>
