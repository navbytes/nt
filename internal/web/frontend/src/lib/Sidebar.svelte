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

<aside class="sidebar" class:sidebar--open={open} aria-label="Sidebar">
  <a class="brand" href="/">
    <svg
      class="brand__mark"
      width="19"
      height="19"
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
    >
      <defs>
        <linearGradient id="brandGrad" x1="2" y1="2" x2="22" y2="22" gradientUnits="userSpaceOnUse">
          <stop offset="0" stop-color="#1aa0ff" />
          <stop offset="1" stop-color="#8a5cf6" />
        </linearGradient>
      </defs>
      <rect x="3" y="3" width="18" height="18" rx="6" fill="url(#brandGrad)" />
      <path
        d="M8 16V9.5l8 6V9"
        stroke="#fff"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
      />
    </svg>
    <span>nt</span>
  </a>

  <nav class="nav" aria-label="Primary">
    {#each primary as item (item.href)}
      <a class="nav__link" class:active={isActive(item.href)} aria-current={isActive(item.href) ? "page" : undefined} href={item.href}>{item.label}</a>
    {/each}
    <div class="nav__label">Explore</div>
    {#each explore as item (item.href)}
      <a class="nav__link" class:active={isActive(item.href)} aria-current={isActive(item.href) ? "page" : undefined} href={item.href}>{item.label}</a>
    {/each}
    {#if ($viewsQ.data?.views ?? []).length > 0}
      <div class="nav__label">Views</div>
      {#each $viewsQ.data?.views ?? [] as v (v.name)}
        <a
          class="nav__link nav__link--view"
          class:active={activeView === v.name}
          aria-current={activeView === v.name ? "page" : undefined}
          href={`/tasks?view=${encodeURIComponent(v.name)}`}
          title={v.summary}>{v.name}</a
        >
      {/each}
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
  .brand__mark {
    flex: none;
    display: block;
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
