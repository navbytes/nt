<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "./api";
  import TreeItem from "./TreeItem.svelte";

  let { path }: { path: string } = $props();

  const notesQ = createQuery({ queryKey: ["notes"], queryFn: api.notes });

  const nav = [
    { href: "/", label: "Dashboard" },
    { href: "/tasks", label: "Tasks" },
    { href: "/activity", label: "Activity" },
    { href: "/tags", label: "Tags" },
    { href: "/orphans", label: "Orphans" },
  ];
</script>

<aside class="sidebar">
  <a class="brand" href="/">nt</a>

  <nav class="nav">
    {#each nav as item (item.href)}
      <a class="nav__link" class:active={path === item.href} href={item.href}>{item.label}</a>
    {/each}
  </nav>

  <div class="tree">
    <div class="tree__head">Notes</div>
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
