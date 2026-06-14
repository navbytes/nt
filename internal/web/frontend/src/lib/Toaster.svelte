<script lang="ts">
  // Renders the single app toast (see toast.svelte.ts). Mounted once in Shell.
  import { toast, dismissToast } from "./toast.svelte";

  async function runUndo() {
    const u = toast.current?.undo;
    dismissToast();
    await u?.();
  }
</script>

<div class="toastwrap" aria-live="polite">
  {#if toast.current}
    {#key toast.current.id}
      <div class="toast">
        <span class="toast__msg">{toast.current.message}</span>
        {#if toast.current.undo}
          <button class="toast__undo" onclick={runUndo}>Undo</button>
        {/if}
        <button class="toast__close" aria-label="Dismiss" onclick={dismissToast}>×</button>
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
    z-index: 70;
    pointer-events: none; /* the empty live region must never block clicks */
  }
  .toast {
    pointer-events: auto;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 9px 12px 9px 16px;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.25);
    font-size: 0.88rem;
    color: var(--fg);
    animation: toast-in 0.15s ease-out;
  }
  @media (prefers-reduced-motion: reduce) {
    .toast {
      animation: none;
    }
  }
  @keyframes toast-in {
    from {
      opacity: 0;
      transform: translateY(6px);
    }
    to {
      opacity: 1;
      transform: none;
    }
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
    color: #fff;
  }
  .toast__close {
    background: none;
    border: none;
    color: var(--muted);
    cursor: pointer;
    font-size: 1rem;
    padding: 0 2px;
  }
  .toast__close:hover {
    color: var(--fg);
  }
</style>
