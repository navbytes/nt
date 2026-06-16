<script lang="ts">
  // Draggable divider on the sidebar's right edge. Pointer-drag resizes; arrow
  // keys nudge (Shift = larger step), Home/End jump to min/max, double-click
  // resets. Hidden on mobile and when the sidebar is collapsed (see app.css).
  import { sidebar, setWidth, resetWidth, MIN_W, MAX_W } from "./sidebarState.svelte";

  let dragging = false;
  let startX = 0;
  let startW = 0;

  function onPointerDown(e: PointerEvent) {
    if (sidebar.collapsed) return;
    dragging = true;
    startX = e.clientX;
    startW = sidebar.width;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    document.body.classList.add("resizing");
    e.preventDefault();
  }
  function onPointerMove(e: PointerEvent) {
    if (!dragging) return;
    setWidth(startW + (e.clientX - startX));
  }
  function onPointerUp(e: PointerEvent) {
    if (!dragging) return;
    dragging = false;
    document.body.classList.remove("resizing");
    try {
      (e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId);
    } catch {
      /* pointer already released */
    }
  }
  function onKeyDown(e: KeyboardEvent) {
    const step = e.shiftKey ? 48 : 16;
    switch (e.key) {
      case "ArrowLeft":
        e.preventDefault();
        setWidth(sidebar.width - step);
        break;
      case "ArrowRight":
        e.preventDefault();
        setWidth(sidebar.width + step);
        break;
      case "Home":
        e.preventDefault();
        setWidth(MIN_W);
        break;
      case "End":
        e.preventDefault();
        setWidth(MAX_W);
        break;
    }
  }
</script>

<!-- A focusable resize separator (WAI-ARIA window-splitter pattern): the
     tabindex + pointer/key handlers are intentional for the drag affordance. -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex a11y_no_noninteractive_element_interactions -->
<div
  class="resizer"
  role="separator"
  aria-orientation="vertical"
  aria-label="Resize sidebar"
  aria-valuemin={MIN_W}
  aria-valuemax={MAX_W}
  aria-valuenow={sidebar.width}
  tabindex="0"
  onpointerdown={onPointerDown}
  onpointermove={onPointerMove}
  onpointerup={onPointerUp}
  onpointercancel={onPointerUp}
  onkeydown={onKeyDown}
  ondblclick={resetWidth}
></div>

<style>
  .resizer {
    flex: 0 0 6px;
    align-self: stretch;
    cursor: col-resize;
    position: relative;
    z-index: 6;
    /* the visible hairline sits centered in the 6px hit target */
    background: transparent;
  }
  .resizer::before {
    content: "";
    position: absolute;
    inset: 0 2px;
    border-radius: 2px;
    background: transparent;
    transition:
      background var(--motion-fast) var(--ease),
      opacity var(--motion-fast) var(--ease);
  }
  /* A spectral thread lights up the divider on hover/keyboard focus. */
  .resizer:hover::before,
  .resizer:focus-visible::before {
    background: var(--grad-spectral-160);
    box-shadow: 0 0 8px -1px var(--spectral-glow);
  }
  .resizer:focus-visible {
    outline: none;
  }
</style>
