<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { api } from "./api";
  import { navigate, loc } from "./router.svelte";
  import { noteUI } from "./noteUI.svelte";
  import { showToast } from "./toast.svelte";
  import TreeItem from "./TreeItem.svelte";

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
    { href: "/", label: "Today" },
    { href: "/tasks", label: "Tasks" },
    { href: "/notes", label: "Notes" },
  ];
  const explore = [
    { href: "/graph", label: "Graph" },
    { href: "/activity", label: "Activity" },
    { href: "/tags", label: "Tags" },
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

<aside class="sidebar" class:sidebar--open={open}>
  <a class="brand" href="/">nt</a>

  <nav class="nav">
    {#each primary as item (item.href)}
      <a class="nav__link" class:active={isActive(item.href)} href={item.href}>{item.label}</a>
    {/each}
    <div class="nav__label">Explore</div>
    {#each explore as item (item.href)}
      <a class="nav__link" class:active={isActive(item.href)} href={item.href}>{item.label}</a>
    {/each}
    {#if ($viewsQ.data?.views ?? []).length > 0}
      <div class="nav__label">Views</div>
      {#each $viewsQ.data?.views ?? [] as v (v.name)}
        <a
          class="nav__link nav__link--view"
          class:active={activeView === v.name}
          href={`/tasks?view=${encodeURIComponent(v.name)}`}
          title={v.summary}>{v.name}</a
        >
      {/each}
    {/if}
  </nav>

  <div class="tree">
    <div class="tree__head">
      <span>Notes</span>
      <button class="tree__new" title="New note" aria-label="New note" disabled={creating} onclick={openNewNote}>+</button>
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
  .tree__newform input {
    width: 100%;
    margin: 6px 0 4px;
    padding: 4px 8px;
    font-size: 0.82rem;
    background: var(--bg-elev);
    border: 1px solid var(--accent);
    border-radius: var(--radius-sm);
    color: var(--fg);
  }
  .tree__newform input:focus {
    outline: none;
  }
</style>
