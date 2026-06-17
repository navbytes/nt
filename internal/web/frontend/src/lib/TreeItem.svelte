<script lang="ts">
  import type { TreeNode } from "./api";
  import TreeItem from "./TreeItem.svelte";
  import Icon from "./Icon.svelte";
  import { isFolderOpen, toggleFolder } from "./sidebarState.svelte";

  // `level` powers aria-level for the tree semantics (W15); top-level items are 1.
  // `isFirst` marks the very first item in the whole tree so it stays tabbable
  // when no note is active (roving tabindex needs exactly one entry point).
  let {
    node,
    path,
    level = 1,
    isFirst = false,
  }: { node: TreeNode; path: string; level?: number; isFirst?: boolean } = $props();

  // Folder open state is persisted in sidebarState so it survives notes refetches
  // (SSE) and reloads, instead of resetting to open every time (W31).
  const open = $derived(node.isNote ? false : isFolderOpen(node.path));
  const active = $derived(path === node.url);

  // Roving keyboard nav over the flat list of visible treeitems (W15). Focus moves
  // by DOM order so it follows the visual order regardless of nesting depth.
  function treeItems(): HTMLElement[] {
    const root = document.querySelector('[role="tree"]');
    if (!root) return [];
    return [...root.querySelectorAll<HTMLElement>('[role="treeitem"]')].filter(
      (el) => el.offsetParent !== null,
    );
  }
  function focusRel(el: HTMLElement, dir: 1 | -1) {
    const items = treeItems();
    const i = items.indexOf(el);
    const next = items[i + dir];
    next?.focus();
  }
  function onKey(e: KeyboardEvent) {
    const el = e.currentTarget as HTMLElement;
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        focusRel(el, 1);
        break;
      case "ArrowUp":
        e.preventDefault();
        focusRel(el, -1);
        break;
      case "ArrowRight":
        if (!node.isNote && !open) {
          e.preventDefault();
          toggleFolder(node.path);
        }
        break;
      case "ArrowLeft":
        if (!node.isNote && open) {
          e.preventDefault();
          toggleFolder(node.path);
        }
        break;
      case "Enter":
      case " ":
        if (!node.isNote) {
          e.preventDefault();
          toggleFolder(node.path);
        }
        break;
    }
  }
</script>

{#if node.isNote}
  <a
    class="tree__note"
    class:active
    href={node.url}
    role="treeitem"
    aria-level={level}
    aria-selected={active}
    aria-current={active ? "page" : undefined}
    tabindex={active || isFirst ? 0 : -1}
    onkeydown={onKey}>{node.name}</a
  >
{:else}
  <div class="tree__folder">
    <button
      class="tree__toggle"
      role="treeitem"
      aria-level={level}
      aria-selected="false"
      aria-expanded={open}
      tabindex={isFirst ? 0 : -1}
      onclick={() => toggleFolder(node.path)}
      onkeydown={onKey}
      aria-label={`Toggle folder ${node.name}`}
    >
      <span class="tree__caret" class:tree__caret--open={open}>
        <Icon name="chevron-right" size={13} />
      </span>{node.name}
    </button>
    {#if open && node.children}
      <div class="tree__children" role="group">
        {#each node.children as child (child.path + child.url)}
          <TreeItem node={child} {path} level={level + 1} />
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
  /* Active note: accent tint + an AA-legible accent label (finding 4 — the old
     --spectral-2 failed AA on the tint). */
  .tree__note.active {
    background: var(--accent-tint);
    color: var(--accent-on-tint);
    font-weight: 600;
  }

  /* The guide line for nested children picks up a faint spectral tint. */
  .tree__children {
    margin-left: 11px;
    border-left: 1px solid var(--separator);
    padding-left: 3px;
  }
</style>
