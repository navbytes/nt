<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { api } from "./api";
  import { navigate } from "./router.svelte";
  import TreeItem from "./TreeItem.svelte";

  let {
    path,
    canEdit = false,
    open = false,
  }: { path: string; canEdit?: boolean; open?: boolean } = $props();

  const qc = useQueryClient();
  const notesQ = createQuery({ queryKey: ["notes"], queryFn: api.notes });

  let creating = $state(false);
  async function newNote() {
    const title = prompt("New note title (use folder/Title to file it in a subfolder):");
    if (!title || !title.trim() || creating) return;
    creating = true;
    try {
      const res = await api.noteCreate(title.trim());
      await qc.invalidateQueries({ queryKey: ["notes"] });
      navigate(res.url);
    } catch (e) {
      alert("Couldn't create the note: " + String(e));
    } finally {
      creating = false;
    }
  }

  // Two tiers: the places you work, then the lenses you look through. Keeps the
  // daily-driver destinations visually distinct from the utility views (audit).
  const primary = [
    { href: "/", label: "Dashboard" },
    { href: "/tasks", label: "Tasks" },
    { href: "/notes", label: "Notes" },
    { href: "/journal", label: "Journal" },
    { href: "/graph", label: "Graph" },
  ];
  const lenses = [
    { href: "/activity", label: "Activity" },
    { href: "/tags", label: "Tags" },
  ];
</script>

<aside class="sidebar" class:sidebar--open={open}>
  <a class="brand" href="/">nt</a>

  <nav class="nav">
    {#each primary as item (item.href)}
      <a class="nav__link" class:active={path === item.href} href={item.href}>{item.label}</a>
    {/each}
    <div class="nav__label">Lenses</div>
    {#each lenses as item (item.href)}
      <a class="nav__link" class:active={path === item.href} href={item.href}>{item.label}</a>
    {/each}
  </nav>

  <div class="tree">
    <div class="tree__head">
      <span>Notes</span>
      {#if canEdit}
        <button class="tree__new" title="New note" aria-label="New note" disabled={creating} onclick={newNote}>+</button>
      {/if}
    </div>
    {#if $notesQ.isPending}
      <p class="muted small">Loading…</p>
    {:else if $notesQ.data}
      {#each $notesQ.data.tree as node (node.path + node.url)}
        <TreeItem {node} {path} />
      {/each}
      {#if $notesQ.data.tree.length === 0}
        <p class="muted small">No notes yet.</p>
      {/if}
    {/if}
  </div>
</aside>

<style>
  .tree__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .tree__new {
    background: none;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-soft);
    cursor: pointer;
    width: 20px;
    height: 20px;
    line-height: 1;
    font-size: 1rem;
    padding: 0;
  }
  .tree__new:hover {
    border-color: var(--accent);
    color: var(--accent);
  }
</style>
