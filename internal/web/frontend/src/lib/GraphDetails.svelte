<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "./api";
  import Icon from "./Icon.svelte";
  import type { FGNode } from "./graph";

  let {
    node,
    pinned,
    expanded,
    onOpen,
    onFocusLocal,
    onTogglePin,
    onToggleExpand,
    onClose,
  }: {
    node: FGNode;
    pinned: boolean;
    expanded: boolean;
    onOpen: () => void;
    onFocusLocal: () => void;
    onTogglePin: () => void;
    onToggleExpand: () => void;
    onClose: () => void;
  } = $props();

  // Only notes have a fetchable body + backlinks. Task nodes are NOT notes, so
  // api.note(taskId) 404s — that was the console 404 when a task was selected in the
  // graph. Gate the fetch on kind so tasks never hit /api/notes.
  const isNote = $derived(node.kind !== "task");
  // Lazily fetch the note for the preview + backlink count. Keyed on the node id
  // so selecting a different node refetches; reuses the same cache as NoteView.
  const noteQ = $derived(
    createQuery({
      queryKey: ["note", node.id],
      queryFn: () => api.note(node.id),
      enabled: isNote,
    }),
  );

  // Plain-text preview from the server-rendered HTML (strips tags, drops a
  // leading H1 that duplicates the title).
  function preview(html: string): string {
    if (!html || typeof DOMParser === "undefined") return "";
    const doc = new DOMParser().parseFromString(html, "text/html");
    doc.querySelector("h1")?.remove();
    const text = (doc.body.textContent ?? "").replace(/\s+/g, " ").trim();
    return text.length > 240 ? text.slice(0, 240) + "…" : text;
  }
</script>

<aside class="gdetails" aria-label="Selected note">
  <div class="gdetails__head">
    <div class="gdetails__crumb">{node.folder || "root"}</div>
    <button class="icon-btn gdetails__close" title="Close (Esc)" aria-label="Close details" onclick={onClose}><Icon name="close" /></button>
  </div>
  <h2 class="gdetails__title">{node.title}</h2>

  <div class="gdetails__meta">
    {#if node.source}<span class="gdetails__src">{node.source}</span>{/if}
    <span class="gdetails__stat">{node.deg} link{node.deg === 1 ? "" : "s"}</span>
    {#if $noteQ.data}<span class="gdetails__stat">{$noteQ.data.backlinks?.length ?? 0} backlink{($noteQ.data.backlinks?.length ?? 0) === 1 ? "" : "s"}</span>{/if}
    {#if pinned}<span class="gdetails__pin"><Icon name="star" size={11} filled={true} /> pinned</span>{/if}
  </div>

  {#if node.tags?.length}
    <div class="gdetails__tags">
      {#each node.tags as t (t)}<span class="gtag">#{t}</span>{/each}
    </div>
  {/if}

  {#if $noteQ.isLoading}
    <p class="muted small">Loading preview…</p>
  {:else if $noteQ.data}
    <p class="gdetails__preview">{preview($noteQ.data.bodyHTML)}</p>
  {/if}

  <div class="gdetails__actions">
    <button class="btn btn--sm" onclick={onOpen}>Open note</button>
    <button class="btn btn--ghost btn--sm gdetails__act" onclick={onFocusLocal}>
      <Icon name="focus" size={14} /> Focus local
    </button>
    <button
      class="btn btn--ghost btn--sm gdetails__act"
      title="Reveal this node's neighbors (shift-click a node does this too)"
      onclick={onToggleExpand}>
      <span class="gdetails__chev" class:gdetails__chev--open={expanded}><Icon name="chevron-right" size={13} /></span>
      {expanded ? "Collapse" : "Expand"} neighbors</button
    >
    <button class="btn btn--ghost btn--sm gdetails__act" onclick={onTogglePin}>
      <Icon name="star" size={13} filled={pinned} /> {pinned ? "Unpin" : "Pin"}
    </button>
  </div>
</aside>

<style>
  /* Glass detail panel — floats over the canvas with the spatial float shadow +
     top light-catch hairline, plus a faint spectral top rule for identity. */
  .gdetails {
    position: absolute;
    top: 12px;
    right: 12px;
    z-index: 20;
    width: 300px;
    max-width: 40vw;
    background: color-mix(in srgb, var(--bg-elevated) 85%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    border: 0.5px solid var(--separator);
    border-radius: var(--radius-popover);
    box-shadow: var(--shadow-float), var(--glass-hairline);
    padding: var(--space-4) var(--space-5) var(--space-5);
    /* Cap to the stage (the positioned ancestor) and scroll within, so long
       note details can't grow past the stage and get clipped by its overflow. */
    max-height: calc(100% - 24px);
    overflow: hidden auto;
    animation: gdetails-in var(--motion) var(--ease-out);
  }
  /* spectral hairline along the top edge — the panel's identity cue */
  .gdetails::before {
    content: "";
    position: absolute;
    inset: 0 0 auto 0;
    height: 2px;
    background: var(--grad-spectral);
    opacity: 0.85;
  }
  @keyframes gdetails-in {
    from {
      opacity: 0;
      transform: translateY(-6px);
    }
    to {
      opacity: 1;
      transform: none;
    }
  }
  .gdetails__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
  }
  .gdetails__crumb {
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .gdetails__close {
    flex: none;
    min-width: 24px;
    min-height: 24px;
    border-color: transparent;
    background: transparent;
  }
  .gdetails__title {
    font-family: var(--font-serif);
    margin: var(--space-1) 0 var(--space-3);
    font-size: var(--text-title1);
    line-height: var(--leading-tight);
    letter-spacing: var(--tracking-title);
    color: var(--fg);
  }
  .gdetails__meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-3);
    margin-bottom: var(--space-3);
  }
  /* mono microlabels for the source + counts */
  .gdetails__src,
  .gdetails__stat {
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    color: var(--muted);
  }
  .gdetails__src {
    color: var(--label-secondary);
  }
  .gdetails__pin {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    color: var(--spectral-2);
  }
  .gdetails__tags {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    margin-bottom: var(--space-3);
  }
  /* spectral-tinted tag chip (matches the Tags screen's vocabulary) */
  .gtag {
    font-size: var(--text-callout);
    padding: 1px 8px;
    border-radius: 999px;
    color: var(--fg);
    background: color-mix(in srgb, var(--spectral-2) 13%, transparent);
    border: 0.5px solid color-mix(in srgb, var(--spectral-2) 30%, transparent);
  }
  .gdetails__preview {
    font-size: var(--text-body);
    color: var(--label-secondary);
    line-height: var(--leading-prose);
    margin: 0 0 var(--space-4);
  }
  .gdetails__actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }
  /* Buttons that lead with an icon: center the glyph against the label. */
  .gdetails__act {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  /* Expand/collapse disclosure: chevron points right, rotates down when the
     neighbors are revealed (neutralized under reduced-motion globally). */
  .gdetails__chev {
    display: inline-flex;
    transition: transform var(--motion-fast) var(--ease);
  }
  .gdetails__chev--open {
    transform: rotate(90deg);
  }

  /* Mobile: the fixed 300px/40vw top-right panel overlaps the controls and
     sandwiches the canvas. Dock it as a full-width bottom sheet instead (audit
     #8) — full width, no max-width cap, pinned to the bottom edge, capped height
     with internal scroll. Sits above the controls bar (higher z-index). */
  @media (max-width: 640px) {
    .gdetails {
      top: auto;
      bottom: 0;
      left: 0;
      right: 0;
      width: auto;
      max-width: none;
      z-index: 22;
      border-radius: var(--radius-popover) var(--radius-popover) 0 0;
      max-height: 60dvh;
      overflow-y: auto;
    }
  }
</style>
