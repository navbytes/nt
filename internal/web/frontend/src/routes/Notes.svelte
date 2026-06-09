<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import type { NoteCard } from "../lib/api";

  const gridQ = createQuery({ queryKey: ["notesGrid"], queryFn: api.notesGrid });

  // Persisted view controls.
  let dense = $state(localStorage.getItem("nt-notes-dense") === "1");
  let folder = $state("");
  let sort = $state<"updated" | "title" | "folder">(
    (localStorage.getItem("nt-notes-sort") as "updated" | "title" | "folder") ?? "updated",
  );
  $effect(() => localStorage.setItem("nt-notes-dense", dense ? "1" : "0"));
  $effect(() => localStorage.setItem("nt-notes-sort", sort));

  const cards = $derived.by((): NoteCard[] => {
    let ns = [...($gridQ.data?.notes ?? [])];
    if (folder) ns = ns.filter((n) => n.folder === folder || n.folder.startsWith(folder + "/"));
    ns.sort((a, b) => {
      if (sort === "title") return a.title.localeCompare(b.title);
      if (sort === "folder") return a.folder.localeCompare(b.folder) || a.title.localeCompare(b.title);
      return (b.updated ?? "").localeCompare(a.updated ?? ""); // newest first
    });
    return ns;
  });
</script>

<div class="pagehead">
  <h1>Notes</h1>
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
    {/if}
  </div>
</div>

{#if $gridQ.isPending}
  <p class="muted">Loading…</p>
{:else if $gridQ.error}
  <p class="error">Couldn't load notes.</p>
{:else if cards.length === 0}
  <p class="muted">No notes{folder ? ` in ${folder}` : ""} yet.</p>
{:else}
  <div class="notegrid" class:notegrid--dense={dense}>
    {#each cards as n (n.handle)}
      <a class="notecard" href={n.url}>
        <div class="notecard__top">
          <span class="notecard__title">{n.title}</span>
          {#if n.updated}<span class="notecard__date">{n.updated}</span>{/if}
        </div>
        {#if n.folder}<span class="notecard__folder">{n.folder}/</span>{/if}
        {#if !dense && n.preview}<p class="notecard__preview">{n.preview}</p>{/if}
        {#if n.tags && n.tags.length}
          <div class="notecard__tags">
            {#each n.tags as t (t)}<span class="chip">#{t}</span>{/each}
          </div>
        {/if}
      </a>
    {/each}
  </div>
{/if}

<style>
  .notes-controls {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
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
