<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api, type NoteLink } from "./api";
  import { navigate } from "./router.svelte";
  import { palette, openPalette, closePalette } from "./palette.svelte";

  let q = $state("");
  let active = $state(0);
  let inputEl: HTMLInputElement | undefined = $state();

  // Reuses the cached note index, so filtering is instant and offline.
  const notesQ = createQuery({ queryKey: ["notes"], queryFn: api.notes });

  type Item = { label: string; sub?: string; url: string; kind: string };

  const navItems: Item[] = [
    { label: "Dashboard", url: "/", kind: "go" },
    { label: "Tasks", url: "/tasks", kind: "go" },
    { label: "Activity", url: "/activity", kind: "go" },
  ];

  const items = $derived(build(q, $notesQ.data?.index ?? []));

  function build(query: string, index: NoteLink[]): Item[] {
    const ql = query.trim().toLowerCase();
    const match = (s: string) => s.toLowerCase().includes(ql);
    const nav = navItems.filter((n) => !ql || match(n.label));
    const notes: Item[] = index
      .filter((n) => !ql || match(n.title) || match(n.path))
      .slice(0, 8)
      .map((n) => ({ label: n.title, sub: n.path, url: n.url, kind: "note" }));
    const out = [...nav, ...notes];
    if (ql) out.push({ label: `Search notes for “${query}”`, url: `/search?q=${encodeURIComponent(query)}`, kind: "search" });
    return out;
  }

  // Keep the highlighted row in range as the result set shrinks/grows.
  $effect(() => {
    if (active > items.length - 1) active = Math.max(0, items.length - 1);
  });

  // Reset + focus whenever the palette opens.
  $effect(() => {
    if (palette.open) {
      q = "";
      active = 0;
      queueMicrotask(() => inputEl?.focus());
    }
  });

  function choose(it: Item | undefined) {
    if (!it) return;
    closePalette();
    navigate(it.url);
  }

  function onGlobalKey(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && (e.key === "k" || e.key === "K")) {
      e.preventDefault();
      palette.open ? closePalette() : openPalette();
    }
  }

  function onInputKey(e: KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      active = Math.min(active + 1, items.length - 1);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      active = Math.max(active - 1, 0);
    } else if (e.key === "Enter") {
      e.preventDefault();
      choose(items[active]);
    } else if (e.key === "Escape") {
      e.preventDefault();
      closePalette();
    }
  }

  $effect(() => {
    window.addEventListener("keydown", onGlobalKey);
    return () => window.removeEventListener("keydown", onGlobalKey);
  });
</script>

{#if palette.open}
  <div class="palette__backdrop" onclick={closePalette} role="presentation">
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div class="palette" role="dialog" aria-modal="true" onclick={(e) => e.stopPropagation()}>
      <input
        bind:this={inputEl}
        bind:value={q}
        onkeydown={onInputKey}
        class="palette__input"
        placeholder="Search notes, jump to…"
        autocomplete="off"
        spellcheck="false"
      />
      <ul class="palette__list">
        {#each items as it, i (it.kind + it.url)}
          <li>
            <button
              class="palette__item"
              class:active={i === active}
              onmouseenter={() => (active = i)}
              onclick={() => choose(it)}
            >
              <span class="palette__kind">{it.kind}</span>
              <span class="palette__label">{it.label}</span>
              {#if it.sub}<span class="palette__sub">{it.sub}</span>{/if}
            </button>
          </li>
        {:else}
          <li class="palette__empty muted">No matches</li>
        {/each}
      </ul>
      <div class="palette__hint muted">↑↓ navigate · ↵ open · esc close</div>
    </div>
  </div>
{/if}
