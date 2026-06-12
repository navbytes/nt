<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import type { NoteCard } from "../lib/api";
  import { loc, navigate } from "../lib/router.svelte";
  import Journal from "./Journal.svelte";

  let { canEdit = false }: { canEdit?: boolean } = $props();

  // Daily (journal) is a view of Notes, selected by the /journal path. The grid
  // is the default; the header toggle switches between them.
  const daily = $derived(loc.path === "/journal");

  const gridQ = createQuery({ queryKey: ["notesGrid"], queryFn: api.notesGrid });
  // Orphans (notes with no links in or out) fold in here as a filter, rather
  // than a separate top-level route.
  const orphansQ = createQuery({ queryKey: ["orphans"], queryFn: api.orphans });
  const orphanUrls = $derived(new Set(($orphansQ.data?.notes ?? []).map((n) => n.url)));

  // Archived notes ride along in the grid payload (flagged), hidden by default;
  // the Archived toggle reveals them — a dedicated view of the retired set.
  const archivedCount = $derived(($gridQ.data?.notes ?? []).filter((n) => n.archived).length);

  // Favorites are the starred working-set notes; the Favorites filter narrows
  // the grid to them. (Star/unstar happens on a note's own page.)
  const favoriteCount = $derived(
    ($gridQ.data?.notes ?? []).filter((n) => n.favorite && !n.archived).length,
  );

  // Persisted view controls.
  let dense = $state(localStorage.getItem("nt-notes-dense") === "1");
  let folder = $state("");
  let orphansOnly = $state(false);
  let archivedOnly = $state(false);
  let favoritesOnly = $state(false);
  let sort = $state<"updated" | "title" | "folder">(
    (localStorage.getItem("nt-notes-sort") as "updated" | "title" | "folder") ?? "updated",
  );
  $effect(() => localStorage.setItem("nt-notes-dense", dense ? "1" : "0"));
  $effect(() => localStorage.setItem("nt-notes-sort", sort));

  const cards = $derived.by((): NoteCard[] => {
    let ns = [...($gridQ.data?.notes ?? [])];
    // Default view is the working set; the Archived toggle flips to retired only.
    ns = ns.filter((n) => (archivedOnly ? n.archived : !n.archived));
    if (folder) ns = ns.filter((n) => n.folder === folder || n.folder.startsWith(folder + "/"));
    if (orphansOnly) ns = ns.filter((n) => orphanUrls.has(n.url));
    if (favoritesOnly) ns = ns.filter((n) => n.favorite);
    ns.sort((a, b) => {
      if (sort === "title") return a.title.localeCompare(b.title);
      if (sort === "folder") return a.folder.localeCompare(b.folder) || a.title.localeCompare(b.title);
      return (b.updated ?? "").localeCompare(a.updated ?? ""); // newest first
    });
    return ns;
  });
</script>

<div class="pagehead">
  <div class="notes-head">
    <h1>Notes</h1>
    <div class="seg" role="group" aria-label="Notes view">
      <button class:seg--on={!daily} onclick={() => navigate("/notes")}>All</button>
      <button class:seg--on={daily} onclick={() => navigate("/journal")}>Daily</button>
    </div>
  </div>
  {#if !daily}
    <div class="notes-controls">
      {#if $gridQ.data}
        <select class="select" bind:value={folder} aria-label="Filter by folder">
        <option value="">All folders</option>
        {#each $gridQ.data.folders as f (f)}<option value={f}>{f}</option>{/each}
      </select>
      <select class="select" bind:value={sort} aria-label="Sort notes">
        <option value="updated">Updated</option>
        <option value="title">Title</option>
        <option value="folder">Folder</option>
      </select>
      <div class="seg" role="group" aria-label="Card density">
        <button class:seg--on={!dense} onclick={() => (dense = false)}>Cards</button>
        <button class:seg--on={dense} onclick={() => (dense = true)}>Compact</button>
      </div>
      {#if favoriteCount > 0 || favoritesOnly}
        <button
          class="notes-toggle"
          class:notes-toggle--on={favoritesOnly}
          aria-pressed={favoritesOnly}
          title="Show only starred notes"
          onclick={() => (favoritesOnly = !favoritesOnly)}
        >★ Favorites{#if favoriteCount}<span class="notes-toggle__count"> {favoriteCount}</span>{/if}</button>
      {/if}
      <button
        class="notes-toggle"
        class:notes-toggle--on={orphansOnly}
        aria-pressed={orphansOnly}
        title="Show only notes with no links in or out"
        onclick={() => (orphansOnly = !orphansOnly)}
      >Orphans{#if $orphansQ.data?.notes.length}<span class="notes-toggle__count"> {$orphansQ.data.notes.length}</span>{/if}</button>
      {#if archivedCount > 0 || archivedOnly}
        <button
          class="notes-toggle"
          class:notes-toggle--on={archivedOnly}
          aria-pressed={archivedOnly}
          title="Show retired notes (hidden from the sidebar, search, and graph)"
          onclick={() => (archivedOnly = !archivedOnly)}
        >📦 Archived{#if archivedCount}<span class="notes-toggle__count"> {archivedCount}</span>{/if}</button>
      {/if}
    {/if}
    </div>
  {/if}
</div>

{#if daily}
  <Journal {canEdit} />
{:else if $gridQ.isPending}
  <p class="muted">Loading…</p>
{:else if $gridQ.error}
  <p class="error">Couldn't load notes.</p>
{:else if cards.length === 0}
  {#if orphansOnly}
    <p class="muted">No orphan notes — every note is linked. ✨</p>
  {:else if archivedOnly}
    <p class="muted">No archived notes.</p>
  {:else if folder}
    <p class="muted">No notes in {folder} yet.</p>
  {:else}
    <div class="empty">
      <p class="empty__lead">No notes yet.</p>
      <p class="muted">
        Notes are your durable memory — the “why” behind decisions, shared with your AI agent.
        Create one with the <strong>＋</strong> in the sidebar, or run <code>nt mcp install</code> so an
        agent can capture them via <code>nt_note</code>.
      </p>
    </div>
  {/if}
{:else}
  <div class="notegrid" class:notegrid--dense={dense}>
    {#each cards as n (n.handle)}
      <a class="notecard" href={n.url}>
        <div class="notecard__top">
          <span class="notecard__title">{#if n.favorite}<span class="notecard__star" title="Favorite">★</span>{/if}{n.title}</span>
          {#if n.updated}<span class="notecard__date">{n.updated}</span>{/if}
        </div>
        {#if n.folder}<span class="notecard__folder">{n.folder}/</span>{/if}
        {#if !dense && n.preview}<p class="notecard__preview">{n.preview}</p>{/if}
        {#if n.tags && n.tags.length}
          <div class="notecard__tags">
            {#each n.tags.slice(0, 4) as t (t)}<span class="chip">#{t}</span>{/each}
            {#if n.tags.length > 4}<span class="chip chip--more" title={n.tags.join(" ")}>+{n.tags.length - 4}</span>{/if}
          </div>
        {/if}
      </a>
    {/each}
  </div>
{/if}

<style>
  .notes-head {
    display: flex;
    align-items: center;
    gap: 16px;
  }
  .notes-controls {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  /* Segmented density control + the orphans toggle, sharing one pill look. */
  .seg {
    display: flex;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }
  .seg button {
    padding: 5px 12px;
    background: var(--bg-inset);
    border: none;
    color: var(--fg-soft);
    cursor: pointer;
    font-size: 0.8rem;
  }
  .seg button.seg--on {
    background: var(--accent);
    color: #fff;
  }
  .notes-toggle {
    padding: 5px 12px;
    background: var(--bg-inset);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-soft);
    cursor: pointer;
    font-size: 0.8rem;
  }
  .notes-toggle--on {
    background: var(--accent);
    color: #fff;
    border-color: var(--accent);
  }
  .notes-toggle__count {
    opacity: 0.8;
  }
  .notegrid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
    gap: 12px;
    margin-top: 16px;
  }
  .notegrid--dense {
    grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
    gap: 8px;
  }
  .notecard {
    display: block;
    padding: 12px 14px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg-elev);
    color: var(--fg);
    transition:
      border-color 0.12s,
      transform 0.12s;
  }
  .notecard:hover {
    border-color: var(--accent);
    text-decoration: none;
    transform: translateY(-1px);
  }
  .notegrid--dense .notecard {
    padding: 8px 10px;
  }
  .notecard__top {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
  }
  .notecard__title {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .notecard__star {
    color: #f5b301;
    margin-right: 4px;
  }
  .notecard__date {
    flex: 0 0 auto;
    font-size: 0.7rem;
    color: var(--muted);
    font-family: var(--font-mono);
  }
  .notecard__folder {
    font-size: 0.72rem;
    color: var(--muted);
    font-family: var(--font-mono);
  }
  .notecard__preview {
    margin: 6px 0 0;
    font-size: 0.82rem;
    color: var(--fg-soft);
    line-height: 1.5;
    display: -webkit-box;
    -webkit-line-clamp: 3;
    line-clamp: 3;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .notecard__tags {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-top: 8px;
  }
</style>
