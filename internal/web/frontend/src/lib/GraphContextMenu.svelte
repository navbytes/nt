<script lang="ts">
  import type { FGNode } from "./graph";

  let {
    x,
    y,
    node,
    pinned,
    onOpen,
    onOpenNewTab,
    onFocusLocal,
    onTogglePin,
    onCopyLink,
    onCopyPath,
    onClose,
  }: {
    x: number;
    y: number;
    node: FGNode;
    pinned: boolean;
    onOpen: () => void;
    onOpenNewTab: () => void;
    onFocusLocal: () => void;
    onTogglePin: () => void;
    onCopyLink: () => void;
    onCopyPath: () => void;
    onClose: () => void;
  } = $props();

  // Keep the menu inside the viewport.
  const left = $derived(Math.min(x, window.innerWidth - 210));
  const top = $derived(Math.min(y, window.innerHeight - 240));
</script>

<svelte:window onkeydown={(e) => e.key === "Escape" && onClose()} />

<!-- Backdrop swallows the next click to dismiss. -->
<div
  class="ctx-backdrop"
  role="presentation"
  onclick={onClose}
  oncontextmenu={(e) => {
    e.preventDefault();
    onClose();
  }}
></div>

<div class="ctx-menu" style="left:{left}px; top:{top}px" role="menu">
  <div class="ctx-title" title={node.title}>{node.title}</div>
  <button role="menuitem" onclick={onOpen}>Open note</button>
  <button role="menuitem" onclick={onOpenNewTab}>Open in new tab</button>
  <button role="menuitem" onclick={onFocusLocal}>Focus local graph</button>
  <div class="ctx-sep"></div>
  <button role="menuitem" onclick={onTogglePin}>{pinned ? "Unpin node" : "Pin node"}</button>
  <div class="ctx-sep"></div>
  <button role="menuitem" onclick={onCopyLink}>Copy wikilink</button>
  <button role="menuitem" onclick={onCopyPath}>Copy note path</button>
</div>

<style>
  .ctx-backdrop {
    position: fixed;
    inset: 0;
    z-index: 40;
  }
  .ctx-menu {
    position: fixed;
    z-index: 41;
    min-width: 190px;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    box-shadow: 0 8px 28px rgba(0, 0, 0, 0.28);
    padding: 4px;
    font-size: 0.85rem;
  }
  .ctx-title {
    padding: 5px 8px 7px;
    font-weight: 600;
    color: var(--fg);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 240px;
    border-bottom: 1px solid var(--border-soft);
    margin-bottom: 3px;
  }
  .ctx-menu button {
    display: block;
    width: 100%;
    text-align: left;
    padding: 6px 8px;
    background: none;
    border: none;
    border-radius: var(--radius-sm);
    color: var(--fg-soft);
    cursor: pointer;
    font-size: 0.85rem;
  }
  .ctx-menu button:hover {
    background: var(--bg-inset);
    color: var(--fg);
  }
  .ctx-sep {
    height: 1px;
    background: var(--border-soft);
    margin: 3px 4px;
  }
</style>
