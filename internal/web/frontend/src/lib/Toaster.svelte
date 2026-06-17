<script lang="ts">
  // Renders the single app toast (see toast.svelte.ts). Mounted once in Shell.
  import { toast, dismissToast } from "./toast.svelte";
  import Icon from "./Icon.svelte";
  import { fly } from "svelte/transition";
  import { cubicOut } from "svelte/easing";

  async function runUndo() {
    const u = toast.current?.undo;
    dismissToast();
    await u?.();
  }

  // Svelte transitions aren't covered by the global prefers-reduced-motion CSS
  // rule, so gate the duration ourselves: 0ms = snap in/out, no slide.
  const reduceMotion =
    typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  const flyArgs = { y: 10, duration: reduceMotion ? 0 : 200, easing: cubicOut };
</script>

<div class="toastwrap" aria-live="polite" aria-atomic="true">
  {#if toast.current}
    {#key toast.current.id}
      <div class="toast" transition:fly={flyArgs}>
        <span class="toast__msg">{toast.current.message}</span>
        {#if toast.current.undo}
          <button class="toast__undo" onclick={runUndo}>Undo</button>
        {/if}
        <button class="toast__close" aria-label="Dismiss" onclick={dismissToast}>
          <Icon name="close" size={14} />
        </button>
      </div>
    {/key}
  {/if}
</div>

<style>
  .toastwrap {
    position: fixed;
    left: 50%;
    bottom: 22px;
    transform: translateX(-50%);
    /* z-scale (finding 14): grain 1 · topbar 5 · resizer 6 · drawer-scrim 25 ·
       drawer 30 · fab 40 · search-drop 50 · dialogs 60 · toast 65 · palette 90.
       65 keeps toasts above page chrome but BELOW the modal dialogs (60 → was
       70, which painted over About/Shortcuts). */
    z-index: 65;
    pointer-events: none; /* the empty live region must never block clicks */
  }
  .toast {
    pointer-events: auto;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 9px 12px 9px 16px;
    /* macOS notification material: translucent elevated surface over a blur. */
    background: color-mix(in srgb, var(--bg-elevated) 85%, transparent);
    -webkit-backdrop-filter: blur(18px) saturate(150%);
    backdrop-filter: blur(18px) saturate(150%);
    border: 1px solid var(--control-border); /* finding 6 — toast edge over a blur */
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-popover);
    font-size: 0.88rem;
    color: var(--fg);
  }
  .toast__undo {
    background: none;
    border: 1px solid var(--accent);
    color: var(--accent);
    border-radius: var(--radius-sm);
    padding: 2px 10px;
    cursor: pointer;
    font-size: 0.82rem;
    font-weight: 600;
  }
  .toast__undo:hover {
    background: var(--accent);
    color: var(--on-accent);
  }
  .toast__close {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: none;
    border: none;
    color: var(--muted);
    cursor: pointer;
    padding: 0 2px;
  }
  .toast__close:hover {
    color: var(--fg);
  }
</style>
