<script lang="ts">
  // The About panel (D6): version + store stats + links. Works identically in the
  // browser and the desktop (Wails) shell — both render this same SPA — so it's
  // the cross-surface "About nt" without needing native dialogs. Mounted once in
  // Shell; opened from the command palette.
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "./api";
  import { about } from "./about.svelte";

  const stateQ = createQuery({ queryKey: ["state"], queryFn: api.state });
  const version = $derived($stateQ.data?.version ?? "");
  const openCount = $derived($stateQ.data?.openCount ?? 0);
  const noteCount = $derived($stateQ.data?.noteCount ?? 0);

  let dialogEl: HTMLElement | undefined = $state();
  let restoreFocus: HTMLElement | null = null;
  $effect(() => {
    if (about.open) {
      restoreFocus = document.activeElement as HTMLElement;
      queueMicrotask(() => dialogEl?.focus());
    } else if (restoreFocus) {
      restoreFocus.focus?.();
      restoreFocus = null;
    }
  });

  function onKey(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.preventDefault();
      about.open = false;
    }
  }
</script>

{#if about.open}
  <div class="ab__backdrop" onclick={() => (about.open = false)} role="presentation">
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div
      class="ab"
      role="dialog"
      aria-modal="true"
      aria-labelledby="ab-title"
      tabindex="-1"
      bind:this={dialogEl}
      onkeydown={onKey}
      onclick={(e) => e.stopPropagation()}
    >
      <div class="ab__brand">nt</div>
      <h2 id="ab-title" class="ab__title">Notes &amp; tasks — durable memory for AI sessions</h2>
      <p class="ab__ver">version <code>{version || "—"}</code></p>
      <p class="ab__stats muted">{openCount} open · {noteCount} notes in your store</p>
      <div class="ab__links">
        <a href="https://github.com/navbytes/nt" target="_blank" rel="noopener noreferrer">GitHub</a>
        <a href="https://github.com/navbytes/nt#readme" target="_blank" rel="noopener noreferrer">Docs</a>
        <a href="https://github.com/navbytes/nt/issues/new" target="_blank" rel="noopener noreferrer">Report an issue</a>
      </div>
      <p class="ab__plain muted">Everything lives as plain files — todo.txt tasks + markdown notes — in your store.</p>
      <button class="btn btn--ghost btn--sm ab__close" onclick={() => (about.open = false)}>Close</button>
    </div>
  </div>
{/if}

<style>
  .ab__backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.4);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 60;
    padding: 16px;
  }
  .ab {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    box-shadow: 0 16px 48px rgba(0, 0, 0, 0.3);
    width: min(420px, 100%);
    padding: 24px 24px 18px;
    text-align: center;
  }
  .ab:focus {
    outline: none;
  }
  .ab__brand {
    display: inline-block;
    font-weight: 700;
    font-size: 1.4rem;
    color: var(--accent);
    border-left: 3px solid var(--accent);
    padding-left: 8px;
    margin-bottom: 10px;
  }
  .ab__title {
    margin: 0 0 10px;
    font-size: 1rem;
    font-weight: 600;
  }
  .ab__ver {
    margin: 0 0 4px;
    font-size: 0.85rem;
  }
  .ab__ver code {
    font-size: 0.82rem;
  }
  .ab__stats {
    margin: 0 0 14px;
    font-size: 0.8rem;
  }
  .ab__links {
    display: flex;
    justify-content: center;
    gap: 16px;
    margin-bottom: 14px;
    font-size: 0.85rem;
  }
  .ab__plain {
    font-size: 0.78rem;
    margin: 0 0 16px;
  }
</style>
