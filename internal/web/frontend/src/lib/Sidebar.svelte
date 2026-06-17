<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { api } from "./api";
  import { navigate, loc } from "./router.svelte";
  import { noteUI } from "./noteUI.svelte";
  import { showToast } from "./toast.svelte";
  import TreeItem from "./TreeItem.svelte";
  import Icon from "./Icon.svelte";

  let {
    path,
    open = false,
  }: { path: string; open?: boolean } = $props();

  const qc = useQueryClient();
  const notesQ = createQuery({ queryKey: ["notes"], queryFn: api.notes });
  // Saved smart views (`nt view save …`) — the same named queries the CLI/TUI
  // recall. The section only appears once the user has saved one.
  const viewsQ = createQuery({ queryKey: ["views"], queryFn: api.views });
  const activeView = $derived(path === "/tasks" ? (loc.query.get("view") ?? "") : "");

  // Inline new-note input (no prompt() — webviews don't implement it, and an
  // in-place field is better UX anyway). Opens from the + button or the
  // palette's "New note" (via the noteUI request counter).
  let newOpen = $state(false);
  let newTitle = $state("");
  let newInput: HTMLInputElement | undefined = $state();
  let creating = $state(false);

  function openNewNote() {
    newOpen = true;
    queueMicrotask(() => newInput?.focus());
  }
  let seenNewReq = noteUI.newNoteRequest;
  $effect(() => {
    if (noteUI.newNoteRequest !== seenNewReq) {
      seenNewReq = noteUI.newNoteRequest;
      openNewNote();
    }
  });

  async function createNote(e: SubmitEvent) {
    e.preventDefault();
    const title = newTitle.trim();
    if (!title || creating) return;
    creating = true;
    try {
      const res = await api.noteCreate(title);
      await qc.invalidateQueries({ queryKey: ["notes"] });
      newTitle = "";
      newOpen = false;
      navigate(res.url);
    } catch (err) {
      showToast(`Couldn't create the note: ${String(err)}`);
    } finally {
      creating = false;
    }
  }
  function onNewKey(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.preventDefault();
      newOpen = false;
      newTitle = "";
    }
  }

  // Two tiers: the entities you own (your daily cockpit + the two things you
  // create), then the cross-cutting views you explore them through. Review lives
  // under Tasks and Daily under Notes — they're views, not peers.
  const primary = [
    { href: "/", label: "Today", icon: "sun" },
    { href: "/tasks", label: "Tasks", icon: "square-check" },
    { href: "/notes", label: "Notes", icon: "document" },
  ];
  const explore = [
    { href: "/graph", label: "Graph", icon: "graph" },
    { href: "/activity", label: "Activity", icon: "activity" },
    { href: "/tags", label: "Tags", icon: "tag" },
  ];

  // A nav item is active on its own path, and on the routes of the views nested
  // under it (Review → Tasks, Daily → Notes), so the parent stays highlighted.
  function isActive(href: string): boolean {
    if (path === href) return true;
    if (href === "/tasks" && path === "/review") return true;
    if (href === "/notes" && path === "/journal") return true;
    return false;
  }
</script>

<aside class="sidebar" class:sidebar--open={open} aria-label="Sidebar">
  <a class="brand" href="/" aria-label="nt — home">
    <span class="brand__glyph" aria-hidden="true">
      <svg class="brand__mark" width="26" height="26" viewBox="0 0 32 32" fill="none">
        <defs>
          <linearGradient id="brandGrad" x1="3" y1="4" x2="29" y2="28" gradientUnits="userSpaceOnUse">
            <stop offset="0" stop-color="var(--spectral-1)" />
            <stop offset="0.52" stop-color="var(--spectral-2)" />
            <stop offset="1" stop-color="var(--spectral-3)" />
          </linearGradient>
        </defs>
        <!-- knowledge-graph: 4 nodes joined by spectral edges -->
        <g class="brand__edges" stroke="url(#brandGrad)" stroke-width="1.6" stroke-linecap="round">
          <path d="M8 9.5 22.5 7" />
          <path d="M8 9.5 11 23" />
          <path d="M22.5 7 11 23" />
          <path d="M11 23 24 21" />
          <path d="M22.5 7 24 21" />
        </g>
        <g class="brand__nodes" fill="url(#brandGrad)">
          <circle class="brand__node brand__node--1" cx="8" cy="9.5" r="3" />
          <circle class="brand__node brand__node--2" cx="22.5" cy="7" r="2.4" />
          <circle class="brand__node brand__node--3" cx="11" cy="23" r="2.4" />
          <circle class="brand__node brand__node--4" cx="24" cy="21" r="3.2" />
        </g>
      </svg>
    </span>
    <span class="brand__word">nt</span>
  </a>

  <nav class="nav" aria-label="Primary">
    <div class="nav__label">Plan</div>
    {#each primary as item (item.href)}
      <a class="nav__link" class:active={isActive(item.href)} aria-current={isActive(item.href) ? "page" : undefined} href={item.href}>
        <span class="nav__bar" aria-hidden="true"></span>
        <Icon name={item.icon} size={16} />
        <span class="nav__text">{item.label}</span>
      </a>
    {/each}
    <div class="nav__label">Explore</div>
    {#each explore as item (item.href)}
      <a class="nav__link" class:active={isActive(item.href)} aria-current={isActive(item.href) ? "page" : undefined} href={item.href}>
        <span class="nav__bar" aria-hidden="true"></span>
        <Icon name={item.icon} size={16} />
        <span class="nav__text">{item.label}</span>
      </a>
    {/each}
    {#if ($viewsQ.data?.views ?? []).length > 0}
      <div class="nav__label">Views</div>
      <div class="nav__pills">
        {#each $viewsQ.data?.views ?? [] as v (v.name)}
          <a
            class="nav__pill"
            class:active={activeView === v.name}
            aria-current={activeView === v.name ? "page" : undefined}
            href={`/tasks?view=${encodeURIComponent(v.name)}`}
            title={v.summary}>{v.name}</a
          >
        {/each}
      </div>
    {/if}
  </nav>

  <div class="tree">
    <div class="tree__head">
      <span>Notes</span>
      <button class="tree__new" title="New note" aria-label="New note" disabled={creating} onclick={openNewNote}><Icon name="plus" size={14} /></button>
    </div>
    {#if newOpen}
      <form class="tree__newform" onsubmit={createNote}>
        <input
          bind:this={newInput}
          bind:value={newTitle}
          onkeydown={onNewKey}
          placeholder="Title — or folder/Title  (esc to cancel)"
          aria-label="New note title"
          autocomplete="off"
          disabled={creating}
        />
      </form>
    {/if}
    {#if $notesQ.isPending}
      <p class="muted small">Loading…</p>
    {:else if $notesQ.data}
      {#if $notesQ.data.tree.length > 0}
        <div role="tree" aria-label="Notes">
          {#each $notesQ.data.tree as node, i (node.path + node.url)}
            <TreeItem {node} {path} isFirst={i === 0} />
          {/each}
        </div>
      {:else}
        <p class="muted small">No notes yet.</p>
      {/if}
    {/if}
  </div>
</aside>

<style>
  /* ── Signature brand mark ─────────────────────────────────────────────
     An animated spectral knowledge-graph glyph (4 nodes + edges) beside the
     mono "nt" wordmark. The whole mark breathes with a slow spectral pan; the
     nodes twinkle gently out of phase. transform/opacity only; neutralized
     under reduced-motion (explicit guard for the infinite ambient loops). */
  .brand {
    display: flex;
    align-items: center;
    gap: 9px;
    margin: 4px 6px 20px;
    font-family: var(--font-mono);
    font-weight: 500;
    color: var(--label-primary);
  }
  .brand:hover {
    text-decoration: none;
  }
  .brand__glyph {
    flex: none;
    display: inline-flex;
    width: 26px;
    height: 26px;
    /* a soft spectral aura pooled under the mark */
    filter: drop-shadow(0 2px 7px var(--spectral-glow));
  }
  .brand__mark {
    display: block;
    overflow: visible;
    transform-origin: center;
    animation: brand-breathe 9s var(--ease) infinite alternate;
  }
  .brand__edges {
    opacity: 0.7;
  }
  .brand__node {
    transform-box: fill-box;
    transform-origin: center;
    animation: brand-twinkle 4.2s var(--ease) infinite alternate;
  }
  .brand__node--2 {
    animation-delay: -0.7s;
  }
  .brand__node--3 {
    animation-delay: -1.9s;
  }
  .brand__node--4 {
    animation-delay: -2.8s;
  }
  .brand__word {
    font-size: 1.02rem;
    letter-spacing: 0.01em;
  }
  @keyframes brand-breathe {
    from {
      transform: scale(1) rotate(0deg);
    }
    to {
      transform: scale(1.04) rotate(2deg);
    }
  }
  @keyframes brand-twinkle {
    from {
      opacity: 0.55;
      transform: scale(0.86);
    }
    to {
      opacity: 1;
      transform: scale(1.06);
    }
  }

  /* ── Nav ──────────────────────────────────────────────────────────────
     Mono uppercase section labels; items are Icon + label with a spectral
     gradient left accent bar on the active row (an absolutely-positioned
     child, not a border, so the rounded gradient reads cleanly). */
  .nav {
    display: grid;
    gap: 1px;
    margin-bottom: 16px;
  }
  .nav__label {
    font-family: var(--font-mono);
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    color: var(--label-tertiary);
    padding: 4px 12px 3px;
    margin-top: 10px;
  }
  .nav__label:first-child {
    margin-top: 0;
  }
  .nav__link {
    position: relative;
    display: flex;
    align-items: center;
    gap: 9px;
    padding: 6px 10px 6px 12px;
    border-radius: var(--radius-sm);
    color: var(--label-secondary);
    transition:
      background var(--motion-fast) var(--ease),
      color var(--motion-fast) var(--ease);
  }
  .nav__link :global(.icon) {
    color: var(--label-tertiary);
    transition: color var(--motion-fast) var(--ease);
  }
  .nav__text {
    font-size: var(--text-body);
  }
  .nav__link:hover {
    background: var(--fill-hover);
    color: var(--label-primary);
    text-decoration: none;
  }
  .nav__link:hover :global(.icon) {
    color: var(--label-secondary);
  }
  /* the spectral active bar: a rounded gradient pill hugging the left edge */
  .nav__bar {
    position: absolute;
    left: 3px;
    top: 50%;
    width: 3px;
    height: 0;
    border-radius: 3px;
    background: var(--grad-spectral);
    opacity: 0;
    transform: translateY(-50%);
    transition:
      height var(--motion) var(--ease-spring),
      opacity var(--motion-fast) var(--ease);
  }
  .nav__link.active {
    background: var(--accent-tint);
    color: var(--label-primary);
    font-weight: 600;
  }
  .nav__link.active :global(.icon) {
    color: var(--accent-on-tint); /* finding 4 — AA-legible on the tint well */
  }
  .nav__link.active .nav__bar {
    height: 17px;
    opacity: 1;
  }

  /* Saved-view pills — small glassy chips that harmonize with the nav. */
  .nav__pills {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
    padding: 3px 8px 2px;
  }
  .nav__pill {
    max-width: 100%;
    padding: 2px 9px;
    border-radius: 999px;
    font-size: var(--text-callout);
    color: var(--label-secondary);
    background: var(--fill);
    border: 1px solid var(--control-border); /* finding 6 — the pill edge is the only affordance */
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    transition:
      background var(--motion-fast) var(--ease),
      color var(--motion-fast) var(--ease),
      border-color var(--motion-fast) var(--ease);
  }
  .nav__pill:hover {
    color: var(--label-primary);
    border-color: color-mix(in srgb, var(--control-border) 60%, var(--label-primary));
    text-decoration: none;
  }
  .nav__pill.active {
    color: var(--accent-on-tint); /* finding 4 — AA-legible active label on tint */
    background: var(--accent-tint);
    border-color: color-mix(in srgb, var(--accent-on-tint) 45%, transparent);
    font-weight: 500;
  }

  @media (prefers-reduced-motion: reduce) {
    .brand__mark,
    .brand__node {
      animation: none;
    }
  }

  /* ── Note tree header ─────────────────────────────────────────────────
     Matches the nav's mono uppercase section labels; the + sits flush right. */
  .tree__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-top: 4px;
  }
  .tree__head :global(span) {
    font-family: var(--font-mono);
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    color: var(--label-tertiary);
  }
  .tree__new {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 28px;
    min-height: 28px;
    margin: -4px;
    background: none;
    border: 0;
    border-radius: var(--radius-sm);
    color: var(--label-secondary);
    cursor: pointer;
    padding: 0;
    transition:
      background var(--motion-fast) var(--ease),
      color var(--motion-fast) var(--ease);
  }
  .tree__new:hover {
    background: var(--fill-hover);
    color: var(--label-primary);
  }
  .tree__new:active {
    background: var(--fill-active);
  }
  .tree__new:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .tree__newform input {
    width: 100%;
    margin: 6px 0 4px;
    padding: 4px 8px;
    font-size: var(--text-body);
    background: var(--fill);
    border: 0.5px solid var(--separator-strong);
    border-radius: var(--radius-sm);
    color: var(--fg);
    transition:
      border-color var(--motion-fast) var(--ease),
      box-shadow var(--motion-fast) var(--ease);
  }
  .tree__newform input:focus {
    border-color: var(--accent-color);
  }
</style>
