<script lang="ts">
  // Renders the app toast STACK (see toast.svelte.ts). Mounted once in Shell.
  // Informational toasts stack (newest at the visible bottom edge); the Undo
  // affordance stays single-flight via clearUndoToast in the callers.
  import { toast, dismissToast, type Toast } from "./toast.svelte";
  import Icon from "./Icon.svelte";
  import { fly } from "svelte/transition";
  import { flip } from "svelte/animate";
  import { cubicOut } from "svelte/easing";

  async function runUndo(t: Toast) {
    const u = t.undo;
    dismissToast(t.id);
    await u?.();
  }

  // Svelte transitions aren't covered by the global prefers-reduced-motion CSS
  // rule, so gate the duration ourselves: 0ms = snap in/out, no slide.
  const reduceMotion =
    typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  const dur = reduceMotion ? 0 : 200;
  const flyArgs = { y: 10, duration: dur, easing: cubicOut };
</script>

<div class="toastwrap" aria-live="polite" aria-atomic="false">
  {#each toast.items as t (t.id)}
    <div class="toast" transition:fly={flyArgs} animate:flip={{ duration: dur }}>
      <span class="toast__msg">{t.message}</span>
      {#if t.undo}
        <button class="toast__undo" onclick={() => runUndo(t)}>Undo</button>
      {/if}
      <button class="toast__close" aria-label="Dismiss" onclick={() => dismissToast(t.id)}>
        <Icon name="close" size={14} />
      </button>
    </div>
  {/each}
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
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px; /* stacked toasts: newest at the bottom edge (DOM order) */
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
