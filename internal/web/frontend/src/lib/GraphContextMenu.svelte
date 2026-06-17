<script lang="ts">
  import Icon from "./Icon.svelte";
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

  // Keep the menu inside the viewport. Clamp BOTH axes from the element's actual
  // measured size (not magic numbers), so a long title can't overflow the right
  // edge and the menu never spills off the bottom (audit #11). Falls back to the
  // requested position until the element is measured on mount.
  let menuEl: HTMLDivElement | undefined = $state();
  let elemW = $state(0);
  let elemH = $state(0);
  function clamp(pos: number, viewport: number, elem: number): number {
    return Math.max(8, Math.min(pos, viewport - elem - 8));
  }
  const left = $derived(elemW ? clamp(x, window.innerWidth, elemW) : x);
  const top = $derived(elemH ? clamp(y, window.innerHeight, elemH) : y);

  // Keyboard accessibility: focus the first item on mount, restore focus to the
  // previously-focused element when the menu is destroyed, and provide roving
  // arrow-key navigation between the menuitem buttons.
  $effect(() => {
    const restore = document.activeElement as HTMLElement | null;
    queueMicrotask(() => {
      if (menuEl) {
        const r = menuEl.getBoundingClientRect();
        elemW = r.width;
        elemH = r.height;
      }
      const first = menuEl?.querySelector<HTMLButtonElement>('button[role="menuitem"]');
      first?.focus();
    });
    return () => restore?.focus?.();
  });

  function items(): HTMLButtonElement[] {
    return menuEl
      ? Array.from(menuEl.querySelectorAll<HTMLButtonElement>('button[role="menuitem"]'))
      : [];
  }
  function onMenuKey(e: KeyboardEvent) {
    if (e.key === "ArrowDown" || e.key === "ArrowUp" || e.key === "Home" || e.key === "End") {
      e.preventDefault();
      e.stopPropagation();
      const list = items();
      if (!list.length) return;
      const cur = list.indexOf(document.activeElement as HTMLButtonElement);
      let next: number;
      if (e.key === "Home") next = 0;
      else if (e.key === "End") next = list.length - 1;
      else if (e.key === "ArrowDown") next = cur < 0 ? 0 : (cur + 1) % list.length;
      else next = cur <= 0 ? list.length - 1 : cur - 1;
      list[next]?.focus();
    }
  }
</script>

<svelte:window
  onkeydown={(e) => {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      onClose();
    }
  }}
/>

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

<div
  class="ctx-menu"
  style="left:{left}px; top:{top}px"
  role="menu"
  tabindex="-1"
  bind:this={menuEl}
  onkeydown={onMenuKey}
>
  <div class="ctx-title" title={node.title}>{node.title}</div>
  <button role="menuitem" onclick={onOpen}><Icon name="arrow-right" size={14} /> Open note</button>
  <button role="menuitem" onclick={onOpenNewTab}><Icon name="document" size={14} /> Open in new tab</button>
  <button role="menuitem" onclick={onFocusLocal}><Icon name="focus" size={14} /> Focus local graph</button>
  <div class="ctx-sep"></div>
  <button role="menuitem" onclick={onTogglePin}>
    <Icon name="star" size={14} filled={pinned} /> {pinned ? "Unpin node" : "Pin node"}
  </button>
  <div class="ctx-sep"></div>
  <button role="menuitem" onclick={onCopyLink}><Icon name="tag" size={14} /> Copy wikilink</button>
  <button role="menuitem" onclick={onCopyPath}><Icon name="command" size={14} /> Copy note path</button>
</div>

<style>
  .ctx-backdrop {
    position: fixed;
    inset: 0;
    z-index: 40;
  }
  /* Glass floating menu — translucent panel over the canvas with the float
     shadow + top light-catch hairline (matches the app's popover/glass system). */
  .ctx-menu {
    position: fixed;
    z-index: 41;
    min-width: 196px;
    background: color-mix(in srgb, var(--bg-elevated) 88%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    border: 0.5px solid var(--separator);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-float), var(--glass-hairline);
    padding: var(--space-1);
    font-size: var(--text-body);
    animation: ctx-in var(--motion-fast) var(--ease-out);
    transform-origin: top left;
  }
  @keyframes ctx-in {
    from {
      opacity: 0;
      transform: scale(0.97);
    }
    to {
      opacity: 1;
      transform: scale(1);
    }
  }
  .ctx-title {
    padding: 6px 10px 7px;
    font-weight: 600;
    color: var(--fg);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 240px;
    border-bottom: 0.5px solid var(--separator);
    margin-bottom: 3px;
  }
  .ctx-menu button {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    text-align: left;
    padding: 6px 10px;
    background: none;
    border: none;
    border-radius: var(--radius-sm);
    color: var(--fg-soft);
    cursor: pointer;
    font-size: var(--text-body);
    transition:
      background var(--motion-fast) var(--ease),
      color var(--motion-fast) var(--ease);
  }
  .ctx-menu button :global(.icon) {
    color: var(--muted);
    transition: color var(--motion-fast) var(--ease);
  }
  /* Spectral-tinted hover — a soft wash, not a hard accent fill, so the floating
     menu stays calm over the graph. */
  .ctx-menu button:hover {
    background: color-mix(in srgb, var(--spectral-2) 14%, transparent);
    color: var(--fg);
  }
  .ctx-menu button:hover :global(.icon) {
    color: var(--spectral-2);
  }
  .ctx-sep {
    height: 0.5px;
    background: var(--separator);
    margin: 3px 4px;
  }
</style>
