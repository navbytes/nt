<script lang="ts">
  // Radial mind-map view of a note's own outline. Pure SVG (no force-graph, no
  // new dependency): the layout comes from lib/mindmap's deterministic radial
  // placement, and this component owns only the interaction — pan/zoom, collapse,
  // ring depth, and "jump to the heading in the prose". Theme-aware via CSS vars;
  // honours prefers-reduced-motion through the global transition rules.
  import { onMount } from "svelte";
  import Icon from "./Icon.svelte";
  import { radialLayout, type MapNode } from "./mindmap";
  import { maxDepth, collapseToDepth, type OutlineNode } from "./outline";

  let {
    root,
    truncated = false,
    onJump,
  }: {
    root: OutlineNode;
    truncated?: boolean;
    onJump?: (anchor: string) => void;
  } = $props();

  // --- collapse state -------------------------------------------------------
  let collapsed = $state<Set<string>>(new Set());
  const depth = $derived(maxDepth(root));

  const layout = $derived(radialLayout(root, { collapsed }));

  function toggle(n: MapNode) {
    if (!n.hasChildren) return;
    const next = new Set(collapsed);
    if (next.has(n.id)) next.delete(n.id);
    else next.add(n.id);
    collapsed = next;
  }

  function activate(n: MapNode) {
    // Internal nodes collapse/expand; a leaf (or a heading with an anchor) jumps
    // to its place in the prose. Double-click always jumps when possible.
    if (n.hasChildren) toggle(n);
    else if (n.anchor && onJump) onJump(n.anchor);
  }

  function expandAll() {
    collapsed = new Set();
  }
  function collapseAll() {
    // Collapse everything below the root's immediate children so the trunk shows.
    collapsed = collapseToDepth(root, 1);
  }
  function ringTo(d: number) {
    collapsed = collapseToDepth(root, d);
  }

  // --- pan / zoom via viewBox ----------------------------------------------
  // World coordinates come straight from the layout; we frame them with a
  // viewBox and let the user pan (drag), zoom (wheel/buttons), and pinch (touch).
  let svgEl: SVGSVGElement | undefined = $state();
  let vb = $state({ x: -400, y: -300, w: 800, h: 600 });
  let dragStart = { x: 0, y: 0, vbx: 0, vby: 0 };
  // Active pointers, keyed by pointerId — one = drag-pan, two = pinch-zoom. This
  // is what makes the map usable on touch (no wheel there).
  const pointers = new Map<number, { x: number; y: number }>();
  let pinchDist = 0; // last two-finger distance, world-independent (screen px)

  // hoveredId / focusedId drive label emphasis and the roving-tabindex keyboard
  // model (only the focused node is tab-reachable; arrows move between nodes).
  let hoveredId = $state<string | null>(null);
  let focusedId = $state("root");

  // Fullscreen: the map's default home is the note's (narrow) prose column, so
  // an Expand toggle lifts it to a fixed inset:0 overlay for real working room.
  // A CSS overlay (not the Fullscreen API) works identically in the browser and
  // the desktop webview, and none of the note's ancestors trap position:fixed.
  let expanded = $state(false);
  function toggleExpand() {
    expanded = !expanded;
    // Reframe to the new size once the browser has applied it.
    requestAnimationFrame(() => fit());
  }
  // While expanded: Escape exits, and the page behind is scroll-locked. The
  // effect's cleanup also runs if the component unmounts mid-fullscreen.
  $effect(() => {
    if (!expanded) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        expanded = false;
      }
    };
    window.addEventListener("keydown", onKey, true);
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      window.removeEventListener("keydown", onKey, true);
      document.body.style.overflow = prevOverflow;
    };
  });

  // fit frames the current layout bounds with padding for labels.
  function fit() {
    if (!svgEl) return;
    const pad = 120;
    const bw = Math.max(1, layout.maxX - layout.minX) + pad * 2;
    const bh = Math.max(1, layout.maxY - layout.minY) + pad * 2;
    const rect = svgEl.getBoundingClientRect();
    const aspect = rect.width / Math.max(1, rect.height);
    // Grow the shorter side of the box so world stays undistorted (meet).
    let w = bw;
    let h = bh;
    if (w / h < aspect) w = h * aspect;
    else h = w / aspect;
    vb = {
      x: (layout.minX + layout.maxX) / 2 - w / 2,
      y: (layout.minY + layout.maxY) / 2 - h / 2,
      w,
      h,
    };
  }

  // Fit once mounted and whenever the visible node set changes shape enough that
  // the frame would clip — we refit on explicit expand/collapse-all + resize, not
  // on every collapse (that would yank the camera). First layout fits on mount.
  onMount(() => {
    fit();
    const ro = new ResizeObserver(() => {
      // Preserve world center; only correct aspect so nothing squashes.
      const rect = svgEl?.getBoundingClientRect();
      if (!rect || rect.height === 0) return;
      const aspect = rect.width / rect.height;
      const cx = vb.x + vb.w / 2;
      const cy = vb.y + vb.h / 2;
      let w = vb.w;
      let h = vb.h;
      if (w / h < aspect) w = h * aspect;
      else h = w / aspect;
      vb = { x: cx - w / 2, y: cy - h / 2, w, h };
    });
    if (svgEl) ro.observe(svgEl);
    return () => ro.disconnect();
  });

  function screenToWorld(clientX: number, clientY: number) {
    const rect = svgEl!.getBoundingClientRect();
    return {
      x: vb.x + ((clientX - rect.left) / rect.width) * vb.w,
      y: vb.y + ((clientY - rect.top) / rect.height) * vb.h,
    };
  }

  // zoomAround scales the viewBox by `factor` while keeping the world point under
  // (cx,cy) — screen coords — stationary. Shared by wheel, pinch, and buttons.
  function zoomAround(factor: number, cx: number, cy: number) {
    const p = screenToWorld(cx, cy);
    const w = Math.min(20000, Math.max(120, vb.w * factor));
    const h = w * (vb.h / vb.w);
    vb = {
      x: p.x - ((p.x - vb.x) * w) / vb.w,
      y: p.y - ((p.y - vb.y) * h) / vb.h,
      w,
      h,
    };
  }

  // zoomStep zooms about the viewport centre — for the on-screen +/− buttons,
  // which are the reliable zoom affordance on touch and for keyboard users.
  function zoomStep(factor: number) {
    if (!svgEl) return;
    const rect = svgEl.getBoundingClientRect();
    zoomAround(factor, rect.left + rect.width / 2, rect.top + rect.height / 2);
  }

  function onWheel(e: WheelEvent) {
    e.preventDefault();
    zoomAround(Math.exp(e.deltaY * 0.0015), e.clientX, e.clientY);
  }

  function onPointerDown(e: PointerEvent) {
    if (e.button !== 0 && e.pointerType === "mouse") return;
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    (e.currentTarget as SVGElement).setPointerCapture(e.pointerId);
    if (pointers.size === 1) {
      dragStart = { x: e.clientX, y: e.clientY, vbx: vb.x, vby: vb.y };
    } else if (pointers.size === 2) {
      const [a, b] = [...pointers.values()];
      pinchDist = Math.hypot(a!.x - b!.x, a!.y - b!.y);
    }
  }
  function onPointerMove(e: PointerEvent) {
    if (!pointers.has(e.pointerId) || !svgEl) return;
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    const rect = svgEl.getBoundingClientRect();
    if (pointers.size >= 2) {
      // Pinch: zoom by the change in finger distance, about the pinch midpoint.
      const [a, b] = [...pointers.values()];
      const dist = Math.hypot(a!.x - b!.x, a!.y - b!.y);
      if (pinchDist > 0 && dist > 0) {
        zoomAround(pinchDist / dist, (a!.x + b!.x) / 2, (a!.y + b!.y) / 2);
      }
      pinchDist = dist;
      return;
    }
    // Single pointer: pan.
    const dx = ((e.clientX - dragStart.x) / rect.width) * vb.w;
    const dy = ((e.clientY - dragStart.y) / rect.height) * vb.h;
    vb = { ...vb, x: dragStart.vbx - dx, y: dragStart.vby - dy };
  }
  function onPointerUp(e: PointerEvent) {
    pointers.delete(e.pointerId);
    pinchDist = 0;
    // A lone remaining finger becomes the new pan anchor so pan resumes smoothly.
    if (pointers.size === 1) {
      const [p] = [...pointers.values()];
      dragStart = { x: p!.x, y: p!.y, vbx: vb.x, vby: vb.y };
    }
    try {
      (e.currentTarget as SVGElement).releasePointerCapture(e.pointerId);
    } catch {
      /* pointer already released */
    }
  }

  // --- keyboard navigation (roving tabindex over the visible tree) -----------
  // Node ids are path-encoded ("root", "root.0", "root.0.1"), so parent/child/
  // sibling relationships are pure string ops — no need to thread the tree here.
  // We restrict to *visible* ids (present in the current layout) so arrows never
  // land on a node hidden under a collapsed branch.
  const visibleIds = $derived(new Set(layout.nodes.map((n) => n.id)));

  const parentId = (id: string) => (id === "root" ? null : id.slice(0, id.lastIndexOf(".")));
  function childIds(id: string): string[] {
    const prefix = id + ".";
    return layout.nodes
      .map((n) => n.id)
      .filter((cid) => cid.startsWith(prefix) && !cid.slice(prefix.length).includes("."))
      .sort((a, b) => Number(a.slice(prefix.length)) - Number(b.slice(prefix.length)));
  }
  function siblingIds(id: string): string[] {
    const p = parentId(id);
    return p === null ? ["root"] : childIds(p);
  }

  // If a collapse hid the focused node, pull focus back to the nearest visible
  // ancestor so keyboard focus is never stranded.
  $effect(() => {
    if (visibleIds.has(focusedId)) return;
    let id: string | null = focusedId;
    while (id && !visibleIds.has(id)) id = parentId(id);
    focusedId = id ?? "root";
  });

  function focusNode(id: string | null | undefined) {
    if (!id || !visibleIds.has(id)) return;
    focusedId = id;
    queueMicrotask(() => svgEl?.querySelector<SVGGElement>(`[data-nid="${CSS.escape(id)}"]`)?.focus());
  }

  function onNodeKey(e: KeyboardEvent, n: MapNode) {
    switch (e.key) {
      case "Enter":
      case " ":
        e.preventDefault();
        activate(n);
        return;
      case "ArrowUp":
        e.preventDefault();
        focusNode(parentId(n.id));
        return;
      case "ArrowDown": {
        e.preventDefault();
        if (n.collapsed) toggle(n); // reveal, then step in on the next press
        else focusNode(childIds(n.id)[0]);
        return;
      }
      case "ArrowLeft":
      case "ArrowRight": {
        e.preventDefault();
        const sibs = siblingIds(n.id);
        const i = sibs.indexOf(n.id);
        const j = e.key === "ArrowRight" ? i + 1 : i - 1;
        if (j >= 0 && j < sibs.length) focusNode(sibs[j]);
        return;
      }
      case "Home":
        e.preventDefault();
        focusNode("root");
        return;
    }
  }

  // --- presentation ---------------------------------------------------------
  // Ring colours cycle through the spectral palette so depth reads at a glance.
  const ringColors = [
    "var(--accent-color)",
    "var(--spectral-1)",
    "var(--spectral-2)",
    "var(--spectral-3)",
  ];
  const colorFor = (d: number) => ringColors[Math.min(d, ringColors.length - 1)];
  const radiusFor = (n: MapNode) => (n.kind === "root" ? 9 : n.depth === 1 ? 6 : 4.5);

  // On a dense map, showing every leaf label turns the canvas to mush. Past a
  // threshold we keep labels for the structural spine (root + first two rings)
  // and for collapsed branches, and reveal the rest on hover/focus. Truncation
  // also tightens so surviving labels stay short.
  const dense = $derived(layout.nodes.length > 60);
  function showLabel(n: MapNode): boolean {
    if (!dense) return true;
    return (
      n.depth <= 2 ||
      n.collapsed ||
      n.id === hoveredId ||
      n.id === focusedId ||
      parentId(n.id) === focusedId // children of the focused node, for context
    );
  }
  function label(n: MapNode): string {
    const t = n.text || "(empty)";
    const max = dense ? 24 : 40;
    return t.length > max ? t.slice(0, max - 1) + "…" : t;
  }
  // Labels sit outboard of the spoke; flip anchor on the left half so text never
  // runs back over the map.
  const rightHalf = (n: MapNode) => Math.cos(n.angle) >= -0.01;

  const hiddenTotal = $derived(
    layout.nodes.reduce((s, n) => s + (n.collapsed ? n.hiddenCount : 0), 0),
  );
</script>

<div class="mm" class:mm--full={expanded}>
  <svg
    bind:this={svgEl}
    class="mm__svg"
    viewBox="{vb.x} {vb.y} {vb.w} {vb.h}"
    role="application"
    aria-label="Mind map. Drag to pan, scroll or pinch to zoom, click a branch to collapse it. Arrow keys move between nodes; Enter collapses or jumps."
    onwheel={onWheel}
    onpointerdown={onPointerDown}
    onpointermove={onPointerMove}
    onpointerup={onPointerUp}
    onpointercancel={onPointerUp}
  >
    <!-- edges under nodes -->
    <g class="mm__edges" fill="none">
      {#each layout.edges as e (e.from + ">" + e.to)}
        <path
          d="M {e.x1} {e.y1} C {e.x1} {(e.y1 + e.y2) / 2}, {e.x2} {(e.y1 + e.y2) / 2}, {e.x2} {e.y2}"
          class="mm__edge"
        />
      {/each}
    </g>

    <g class="mm__nodes">
      {#each layout.nodes as n (n.id)}
        {@const r = radiusFor(n)}
        {@const c = colorFor(n.depth)}
        <g
          class="mm__node"
          class:mm__node--collapsed={n.collapsed}
          class:mm__node--focused={n.id === focusedId}
          data-nid={n.id}
          transform="translate({n.x} {n.y})"
          role="button"
          tabindex={n.id === focusedId ? 0 : -1}
          aria-label={`${n.text}${n.hasChildren ? (n.collapsed ? `, collapsed, ${n.hiddenCount} hidden` : ", branch") : ""}`}
          onclick={(e) => {
            e.stopPropagation();
            focusedId = n.id;
            activate(n);
          }}
          ondblclick={(e) => {
            e.stopPropagation();
            if (n.anchor && onJump) onJump(n.anchor);
          }}
          onkeydown={(e) => onNodeKey(e, n)}
          onpointerenter={() => (hoveredId = n.id)}
          onpointerleave={() => hoveredId === n.id && (hoveredId = null)}
        >
          <circle
            class="mm__dot"
            r={r}
            style="fill:{n.collapsed ? 'var(--bg-elevated)' : c}; stroke:{c}"
          />
          {#if n.collapsed}
            <text class="mm__badge" text-anchor="middle" dy="0.32em" style="fill:{c}">+</text>
          {/if}
          {#if showLabel(n)}
            <text
              class="mm__label"
              x={rightHalf(n) ? r + 5 : -(r + 5)}
              dy="0.32em"
              text-anchor={rightHalf(n) ? "start" : "end"}
            >
              {label(n)}{#if n.collapsed}<tspan class="mm__count"> · {n.hiddenCount}</tspan>{/if}
            </text>
          {/if}
        </g>
      {/each}
    </g>
  </svg>

  <!-- controls -->
  <div class="mm__ctl" role="group" aria-label="Mind map controls">
    <div class="mm__seg">
      <button title="Expand all branches" aria-label="Expand all branches" onclick={expandAll}><Icon name="plus" size={14} /></button>
      <button title="Collapse to trunk" aria-label="Collapse to trunk" onclick={collapseAll}><Icon name="close" size={14} /></button>
      <button class="mm__fit" title="Fit to view" aria-label="Fit to view" onclick={fit}><Icon name="focus" size={14} /></button>
    </div>
    <div class="mm__seg" aria-label="Zoom">
      <button class="mm__zoom" title="Zoom out" aria-label="Zoom out" onclick={() => zoomStep(1.3)}>−</button>
      <button class="mm__zoom" title="Zoom in" aria-label="Zoom in" onclick={() => zoomStep(1 / 1.3)}>+</button>
      <button
        class="mm__zoom"
        title={expanded ? "Exit fullscreen (Esc)" : "Fullscreen"}
        aria-label={expanded ? "Exit fullscreen" : "Fullscreen"}
        aria-pressed={expanded}
        onclick={toggleExpand}>{expanded ? "⤡" : "⤢"}</button>
    </div>
    {#if depth > 1}
      <div class="mm__rings" aria-label="Show rings">
        <span class="mm__ringlabel">Rings</span>
        {#each Array.from({ length: depth }, (_, i) => i + 1) as d (d)}
          <button class="mm__ring" onclick={() => ringTo(d)} title={`Show ${d} ring${d > 1 ? "s" : ""}`}>{d}</button>
        {/each}
      </div>
    {/if}
    <p class="mm__stats">
      <strong>{layout.nodes.length}</strong> shown{#if hiddenTotal}<span class="mm__sep"> · </span>{hiddenTotal} hidden{/if}{#if truncated}<span class="mm__sep"> · </span><span class="mm__trunc" title="This note is large; some detail was omitted">capped</span>{/if}
    </p>
    <p class="mm__hint">Click a branch to collapse · double-click to jump · arrow keys to navigate</p>
  </div>
</div>

<style>
  .mm {
    position: relative;
    width: 100%;
    height: min(72vh, 680px);
    min-height: 340px;
    border: 0.5px solid var(--separator);
    border-radius: var(--radius-lg, 12px);
    background:
      radial-gradient(circle at 50% 42%, color-mix(in srgb, var(--spectral-glow, var(--accent-color)) 8%, transparent), transparent 60%),
      var(--bg-inset, var(--bg));
    overflow: hidden;
  }
  /* Fullscreen: lift the map out of the narrow prose column to fill the viewport.
     A plain fixed overlay (none of the note's ancestors trap position:fixed) —
     works the same in the browser and the desktop webview. */
  .mm--full {
    position: fixed;
    inset: 0;
    z-index: 1000;
    width: 100vw;
    height: 100dvh;
    max-height: none;
    border: none;
    border-radius: 0;
  }
  .mm__svg {
    width: 100%;
    height: 100%;
    display: block;
    cursor: grab;
    touch-action: none;
  }
  .mm__svg:active {
    cursor: grabbing;
  }
  .mm__edge {
    stroke: color-mix(in srgb, var(--fg) 24%, transparent);
    stroke-width: 1;
    vector-effect: non-scaling-stroke;
  }
  .mm__node {
    cursor: pointer;
  }
  .mm__node:focus-visible {
    outline: none;
  }
  .mm__node:focus-visible .mm__dot {
    stroke-width: 2.5;
    filter: drop-shadow(0 0 3px var(--focus-ring, var(--accent-color)));
  }
  .mm__dot {
    stroke-width: 1.5;
    transition: r var(--motion-fast, 120ms) var(--ease, ease);
  }
  .mm__node:hover .mm__dot {
    stroke-width: 2.5;
  }
  /* Focused node (roving tabindex / arrow-key target): a persistent ring so
     keyboard users always see where they are, not just on :focus-visible. */
  .mm__node--focused .mm__dot {
    stroke-width: 3;
    filter: drop-shadow(0 0 4px var(--focus-ring, var(--accent-color)));
  }
  .mm__node--focused .mm__label {
    font-weight: 600;
  }
  .mm__badge {
    font-family: var(--font-mono, monospace);
    font-size: 8px;
    font-weight: 700;
    pointer-events: none;
  }
  .mm__label {
    font-family: var(--font-ui, system-ui);
    font-size: 12px;
    fill: var(--fg);
    paint-order: stroke;
    stroke: var(--bg-inset, var(--bg));
    stroke-width: 3px;
    pointer-events: none;
  }
  .mm__count {
    fill: var(--muted);
    font-family: var(--font-mono, monospace);
  }
  .mm__node--collapsed .mm__label {
    fill: var(--fg-soft, var(--fg));
  }

  /* control panel — compact glass strip, echoing the graph cockpit */
  .mm__ctl {
    position: absolute;
    top: 10px;
    left: 10px;
    display: flex;
    flex-direction: column;
    gap: var(--space-2, 6px);
    padding: var(--space-2, 6px) var(--space-3, 8px);
    background: color-mix(in srgb, var(--bg-elevated) 86%, transparent);
    -webkit-backdrop-filter: saturate(1.4) blur(8px);
    backdrop-filter: saturate(1.4) blur(8px);
    border: 0.5px solid var(--separator);
    border-radius: var(--radius-popover, 10px);
    box-shadow: var(--shadow-float, 0 4px 16px rgba(0, 0, 0, 0.15));
    max-width: min(280px, calc(100% - 20px));
  }
  .mm__seg,
  .mm__rings {
    display: flex;
    align-items: center;
    gap: 4px;
    flex-wrap: wrap;
  }
  .mm__seg button,
  .mm__ring {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 26px;
    min-height: 24px;
    padding: 2px 6px;
    background: var(--fill);
    border: 0.5px solid var(--separator-strong, var(--separator));
    border-radius: var(--radius-sm, 6px);
    color: var(--fg-soft, var(--fg));
    cursor: pointer;
    font-size: var(--text-callout, 12px);
    transition:
      background var(--motion-fast, 120ms) var(--ease, ease),
      color var(--motion-fast, 120ms) var(--ease, ease),
      border-color var(--motion-fast, 120ms) var(--ease, ease);
  }
  .mm__seg button:hover,
  .mm__ring:hover {
    color: var(--fg);
    border-color: var(--fg-soft);
  }
  /* Zoom +/- glyphs read larger than the icon buttons; equal-width so they pair. */
  .mm__zoom {
    font-size: 16px;
    font-weight: 600;
    line-height: 1;
    min-width: 30px;
  }
  .mm__ringlabel {
    font-family: var(--font-mono, monospace);
    font-size: var(--text-footnote, 10px);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps, 0.04em);
    color: var(--muted);
  }
  .mm__stats {
    margin: 0;
    color: var(--muted);
    font-family: var(--font-mono, monospace);
    font-size: var(--text-subhead, 11px);
    font-variant-numeric: tabular-nums;
  }
  .mm__stats strong {
    color: var(--label-secondary, var(--fg-soft));
    font-weight: 600;
  }
  .mm__sep {
    color: var(--separator-strong, var(--separator));
  }
  .mm__trunc {
    color: var(--warning, var(--fg-soft));
  }
  .mm__hint {
    margin: 0;
    color: var(--muted);
    font-size: var(--text-footnote, 10px);
    line-height: 1.35;
    max-width: 220px;
  }
  @media (max-width: 640px) {
    .mm {
      height: min(70vh, 560px);
    }
    .mm__hint {
      display: none;
    }
    /* Roomier tap targets on touch (Apple's ~44px guidance) + a translucent
       panel that doesn't hog the small canvas. */
    .mm__seg button,
    .mm__ring,
    .mm__zoom {
      min-width: 34px;
      min-height: 34px;
    }
    .mm__ctl {
      gap: var(--space-1, 4px);
      max-width: calc(100% - 16px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .mm__dot {
      transition: none;
    }
  }
</style>
