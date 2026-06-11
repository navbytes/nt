<script lang="ts">
  // Owns the global keyboard layer (see keys.svelte.ts) and renders the `?`
  // cheat-sheet. Kept separate from the command palette so each surface stays
  // simple; both are mounted once in Shell.
  import { navigate } from "./router.svelte";
  import { openPalette, palette } from "./palette.svelte";
  import { shortcuts, goTarget, isTextEntry, GO_CHORDS } from "./keys.svelte";

  let { canEdit = false }: { canEdit?: boolean } = $props();

  // `g`-chord state: once `g` is pressed we wait briefly for the second key.
  let pendingGo = $state(false);
  let goTimer: ReturnType<typeof setTimeout> | undefined;
  function armGo() {
    pendingGo = true;
    clearTimeout(goTimer);
    goTimer = setTimeout(() => (pendingGo = false), 1200);
  }
  function disarmGo() {
    pendingGo = false;
    clearTimeout(goTimer);
  }

  // `c` capture: focus an on-page quick-add if there is one, else go to Tasks
  // (where the add box lives) so the keystroke always lands somewhere useful.
  function capture() {
    const box = document.querySelector<HTMLInputElement>(".taskadd input");
    if (box) box.focus();
    else navigate("/tasks");
  }

  // `/` search: focus the top-bar search field instead of typing a literal slash.
  function focusSearch() {
    document.querySelector<HTMLInputElement>('.topbar__search input[name="q"]')?.focus();
  }

  function onKey(e: KeyboardEvent) {
    // Never hijack typing, shortcuts with a modifier, or repeats.
    if (e.metaKey || e.ctrlKey || e.altKey) return;

    // Escape closes the cheat-sheet from anywhere.
    if (e.key === "Escape" && shortcuts.open) {
      e.preventDefault();
      shortcuts.open = false;
      return;
    }
    if (isTextEntry(e.target) || palette.open) return;

    // Resolve a pending `g` chord first.
    if (pendingGo) {
      disarmGo();
      const path = goTarget(e.key);
      if (path) {
        e.preventDefault();
        navigate(path);
      }
      return;
    }

    switch (e.key) {
      case "g":
        armGo();
        break;
      case "?":
        e.preventDefault();
        shortcuts.open = !shortcuts.open;
        break;
      case "/":
        e.preventDefault();
        focusSearch();
        break;
      case "c":
        e.preventDefault();
        if (canEdit) capture();
        else openPalette();
        break;
    }
  }

  $effect(() => {
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  // Focus the dialog on open and restore focus to wherever we came from on close.
  let dialogEl: HTMLElement | undefined = $state();
  let restoreFocus: HTMLElement | null = null;
  $effect(() => {
    if (shortcuts.open) {
      restoreFocus = document.activeElement as HTMLElement;
      queueMicrotask(() => dialogEl?.focus());
    } else if (restoreFocus) {
      restoreFocus.focus?.();
      restoreFocus = null;
    }
  });

  const general = $derived([
    { keys: ["⌘", "K"], label: "Command palette" },
    { keys: ["/"], label: "Search" },
    { keys: ["c"], label: canEdit ? "Capture a task" : "Open palette" },
    { keys: ["?"], label: "This cheat-sheet" },
    { keys: ["Esc"], label: "Close / cancel" },
  ]);
</script>

{#if shortcuts.open}
  <div class="sc__backdrop" onclick={() => (shortcuts.open = false)} role="presentation">
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div
      class="sc"
      role="dialog"
      aria-modal="true"
      aria-labelledby="sc-title"
      tabindex="-1"
      bind:this={dialogEl}
      onclick={(e) => e.stopPropagation()}
    >
      <h2 id="sc-title" class="sc__title">Keyboard shortcuts</h2>
      <div class="sc__cols">
        <section>
          <h3 class="sc__head">Go to <span class="sc__prefix">g</span> then…</h3>
          <ul class="sc__list">
            {#each GO_CHORDS as g (g.key)}
              <li><kbd class="kbd">g</kbd> <kbd class="kbd">{g.key}</kbd><span>{g.label}</span></li>
            {/each}
          </ul>
        </section>
        <section>
          <h3 class="sc__head">General</h3>
          <ul class="sc__list">
            {#each general as g (g.label)}
              <li>
                {#each g.keys as k (k)}<kbd class="kbd">{k}</kbd>{/each}
                <span>{g.label}</span>
              </li>
            {/each}
          </ul>
        </section>
      </div>
      <div class="sc__hint muted">Press <kbd class="kbd">?</kbd> or <kbd class="kbd">Esc</kbd> to close</div>
    </div>
  </div>
{/if}

<style>
  .sc__backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.4);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 60;
    padding: 16px;
  }
  .sc {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    box-shadow: 0 16px 48px rgba(0, 0, 0, 0.3);
    width: min(560px, 100%);
    padding: 20px 22px 16px;
  }
  .sc:focus {
    outline: none;
  }
  .sc__title {
    margin: 0 0 14px;
    font-size: 1.05rem;
  }
  .sc__cols {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 22px;
  }
  @media (max-width: 520px) {
    .sc__cols {
      grid-template-columns: 1fr;
    }
  }
  .sc__head {
    margin: 0 0 8px;
    font-size: 0.72rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--muted);
  }
  .sc__prefix {
    color: var(--fg-soft);
  }
  .sc__list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 7px;
  }
  .sc__list li {
    display: flex;
    align-items: center;
    gap: 5px;
    font-size: 0.85rem;
  }
  .sc__list li span {
    margin-left: 6px;
    color: var(--fg-soft);
  }
  .sc__hint {
    margin-top: 16px;
    font-size: 0.76rem;
  }
</style>
