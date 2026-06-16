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
  /* Base item layout/colors come from app.css's global .tree__note / .tree__toggle.
     Here we (a) animate the disclosure caret, (b) give the note/folder rows a
     spectral active treatment harmonized with the nav, and (c) tighten rhythm. */
  .tree__caret {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: var(--label-tertiary);
    transition: color var(--motion-fast) var(--ease);
  }
  .tree__caret :global(.icon) {
    transition: transform var(--motion) var(--ease-spring);
  }
  .tree__caret--open :global(.icon) {
    transform: rotate(90deg);
  }

  .tree__note,
  .tree__toggle {
    border-radius: var(--radius-sm);
  }
  .tree__toggle {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .tree__toggle:hover .tree__caret {
    color: var(--label-secondary);
  }
  /* Active note: accent tint + a spectral-tinted label, matching the nav. */
  .tree__note.active {
    background: var(--accent-tint);
    color: var(--spectral-2);
    font-weight: 600;
  }

  /* The guide line for nested children picks up a faint spectral tint. */
  .tree__children {
    margin-left: 11px;
    border-left: 1px solid var(--separator);
    padding-left: 3px;
  }
</style>
