<script lang="ts">
  import type { TreeNode } from "./api";
  import TreeItem from "./TreeItem.svelte";

  let { node, path }: { node: TreeNode; path: string } = $props();
  let open = $state(true);
</script>

{#if node.isNote}
  <a class="tree__note" class:active={path === node.url} href={node.url}>{node.name}</a>
{:else}
  <div class="tree__folder">
    <button class="tree__toggle" onclick={() => (open = !open)}>
      <span class="tree__caret">{open ? "▾" : "▸"}</span>{node.name}
    </button>
    {#if open && node.children}
      <div class="tree__children">
        {#each node.children as child (child.path + child.url)}
          <TreeItem node={child} {path} />
        {/each}
      </div>
    {/if}
  </div>
{/if}
