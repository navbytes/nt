<script lang="ts">
  import type { TreeNode } from "./api";
  import TreeItem from "./TreeItem.svelte";
  import Icon from "./Icon.svelte";
  import { navigate } from "./router.svelte";
  import {
    isFolderOpen,
    toggleFolder,
    expandFolder,
    collapseFolder,
    treeRoving,
    setRovingItem,
  } from "./sidebarState.svelte";
  import { stepIndex, parentIndex, firstChildIndex, type TreeNavItem } from "./treenav";

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

  // The roving-tabindex key for this item — its url (notes) or folder path. Used
  // both as the DOM id (so nav helpers can .focus() a sibling/parent/child) and as
  // the "which item is the single tab stop" marker.
  const key = $derived(node.isNote ? node.url : node.path);
  const domId = $derived(`tree-${key}`);

  // Exactly ONE treeitem in the whole tree is tabbable (tabindex=0). That's the
  // roving item if one is set; otherwise the active note; otherwise the very first
  // item — so there's always exactly one entry point even before any interaction.
  const tabbable = $derived(
    treeRoving.key !== null ? treeRoving.key === key : active || isFirst,
  );

  // The flat list of VISIBLE treeitems, in DOM (== visual) order. Collapsed
  // folders don't render their children, so this already respects collapse.
  function visibleItems(): HTMLElement[] {
    const root = document.querySelector('[role="tree"]');
    if (!root) return [];
    return [...root.querySelectorAll<HTMLElement>('[role="treeitem"]')].filter(
      (el) => el.offsetParent !== null,
    );
  }
  function navItems(els: HTMLElement[]): TreeNavItem[] {
    return els.map((el) => ({ level: Number(el.getAttribute("aria-level")) || 1 }));
  }
  // Move roving focus to the element at `idx` (no-op if out of range). Updating the
  // roving key flips tabindex=0 to the new item, so the tree stays one tab stop.
  function focusAt(els: HTMLElement[], idx: number) {
    const el = els[idx];
    if (!el) return;
    el.focus();
    el.scrollIntoView({ block: "nearest" });
  }
  function onKey(e: KeyboardEvent) {
    const els = visibleItems();
    const me = e.currentTarget as HTMLElement;
    const i = els.indexOf(me);
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        focusAt(els, stepIndex(els.length, i, 1));
        break;
      case "ArrowUp":
        e.preventDefault();
        focusAt(els, stepIndex(els.length, i, -1));
        break;
      case "ArrowRight":
        e.preventDefault();
        if (node.isNote) break;
        if (!open) {
          expandFolder(node.path); // collapsed → expand in place
        } else {
          const c = firstChildIndex(navItems(els), i); // expanded → first child
          if (c >= 0) focusAt(els, c);
        }
        break;
      case "ArrowLeft":
        e.preventDefault();
        if (!node.isNote && open) {
          collapseFolder(node.path); // expanded folder → collapse in place
        } else {
          const p = parentIndex(navItems(els), i); // else → move to parent
          if (p >= 0) focusAt(els, p);
        }
        break;
      case "Home":
        e.preventDefault();
        focusAt(els, 0);
        break;
      case "End":
        e.preventDefault();
        focusAt(els, els.length - 1);
        break;
      case "Enter":
      case " ":
        e.preventDefault();
        if (node.isNote) navigate(node.url); // activate: open the note
        else toggleFolder(node.path); // activate: toggle the folder
        break;
    }
  }
</script>

{#if node.isNote}
  <a
    id={domId}
    class="tree__note"
    class:active
    href={node.url}
    role="treeitem"
    aria-level={level}
    aria-selected={active}
    aria-current={active ? "page" : undefined}
    tabindex={tabbable ? 0 : -1}
    onfocus={() => setRovingItem(key)}
    onkeydown={onKey}>{node.name}</a
  >
{:else}
  <div class="tree__folder">
    <button
      id={domId}
      class="tree__toggle"
      role="treeitem"
      aria-level={level}
      aria-selected="false"
      aria-expanded={open}
      tabindex={tabbable ? 0 : -1}
      onfocus={() => setRovingItem(key)}
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
