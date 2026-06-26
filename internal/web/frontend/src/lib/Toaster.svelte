<script lang="ts">
  // Renders the app toast STACK (see toast.svelte.ts). Mounted once in Shell.
  // Informational toasts stack (newest at the visible bottom edge); the Undo
  // affordance stays single-flight via clearUndoToast in the callers.
  import { toast, dismissToast, pauseToast, resumeToast, type Toast } from "./toast.svelte";
  import Icon from "./Icon.svelte";
  import { fly } from "svelte/transition";
  import { flip } from "svelte/animate";
  import { cubicOut } from "svelte/easing";

  async function runUndo(t: Toast) {
    const u = t.undo;
    dismissToast(t.id);
    await u?.();
  }

  // Ids whose dismiss clock is paused (hover/focus). Mirrored into the draining
  // bar's animation-play-state so the underline freezes in step with the timer.
  let paused = $state(new Set<number>());
  function pause(id: number) {
    pauseToast(id);
    paused = new Set(paused).add(id);
  }
  function resume(id: number) {
    resumeToast(id);
    const next = new Set(paused);
    next.delete(id);
    paused = next;
  }

  // Svelte transitions aren't covered by the global prefers-reduced-motion CSS
  // rule, so gate the duration ourselves: 0ms = snap in/out, no slide.
  const reduceMotion =
    typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  const dur = reduceMotion ? 0 : 200;
  const flyArgs = { y: 10, duration: dur, easing: cubicOut };
</script>

<!-- Plain confirmations live in this polite region; Undo toasts carry their own
     role="alert" (assertive) below so the reversible action is announced promptly. -->
<div class="toastwrap" aria-live="polite" aria-atomic="false">
  {#each toast.items as t (t.id)}
    <div
      class="toast"
      class:toast--undo={t.undo}
      role={t.undo ? "alert" : undefined}
      transition:fly={flyArgs}
      animate:flip={{ duration: dur }}
      onmouseenter={() => t.undo && pause(t.id)}
      onmouseleave={() => t.undo && resume(t.id)}
      onfocusin={() => t.undo && pause(t.id)}
      onfocusout={() => t.undo && resume(t.id)}
    >
      <span class="toast__msg">{t.message}</span>
      {#if t.undo}
        <button class="toast__undo" onclick={() => runUndo(t)}>Undo</button>
      {/if}
      <button class="toast__close" aria-label="Dismiss" onclick={() => dismissToast(t.id)}>
        <Icon name="close" size={14} />
      </button>
      {#if t.undo && !reduceMotion}
        <!-- A thin draining underline: scales from full to empty over the toast's
             ttl, so the time left to hit Undo is visible. Pausing the dismiss
             timer on hover/focus pauses this too (animation-play-state). The bar
             is decorative (aria-hidden); under reduced-motion it's omitted. -->
        <span
          class="toast__drain"
          aria-hidden="true"
          style="animation-duration: {t.ttlMs}ms"
          class:toast__drain--paused={paused.has(t.id)}
        ></span>
      {/if}
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
    position: relative; /* anchors the draining underline */
    overflow: hidden; /* clip the bar to the rounded corners */
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
  /* Draining underline for Undo toasts: a thin accent bar pinned to the bottom
     edge that scales from full to empty over the toast's ttl, so the shrinking
     time-to-Undo is visible. transform-origin:left → it drains leftward; paused
     in step with the dismiss timer on hover/focus. */
  .toast__drain {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    height: 2px;
    background: var(--accent);
    transform-origin: left center;
    animation-name: toast-drain;
    animation-timing-function: linear;
    animation-fill-mode: forwards;
    will-change: transform;
  }
  .toast__drain--paused {
    animation-play-state: paused;
  }
  @keyframes toast-drain {
    from {
      transform: scaleX(1);
    }
    to {
      transform: scaleX(0);
    }
  }
</style>
