<script lang="ts">
  import type { TreeNode } from "./api";
  import TreeItem from "./TreeItem.svelte";
  import Icon from "./Icon.svelte";

  let { node, path }: { node: TreeNode; path: string } = $props();
  let open = $state(true);
</script>

{#if node.isNote}
  <a class="tree__note" class:active={path === node.url} href={node.url}>{node.name}</a>
{:else}
  <div class="tree__folder">
    <button
      class="tree__toggle"
      onclick={() => (open = !open)}
      aria-label={`Toggle folder ${node.name}`}
      aria-expanded={open}
    >
      <span class="tree__caret" class:tree__caret--open={open}>
        <Icon name="chevron-right" size={13} />
      </span>{node.name}
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

<style>
  /* Color (--label-tertiary via --muted) + 14px slot come from app.css's
     global .tree__caret. We only add the disclosure rotation + glyph centering. */
  .tree__caret {
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .tree__caret :global(.icon) {
    transition: transform var(--motion-fast) var(--ease);
  }
  .tree__caret--open :global(.icon) {
    transform: rotate(90deg);
  }
</style>
