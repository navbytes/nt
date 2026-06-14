<script lang="ts">
  // The top-bar search: results-as-you-type in a dropdown, with the matched
  // query highlighted. Enter (or a click) opens the active result; Enter with no
  // active row opens the full /search page. Reuses the same ranked search API and
  // highlight helper the /search route uses.
  import { api } from "./api";
  import { navigate } from "./router.svelte";
  import { highlightParts } from "./text";
  import type { SearchResult } from "./api-types";

  let q = $state("");
  let debounced = $state("");
  let open = $state(false);
  let active = $state(-1);
  let inputEl: HTMLInputElement | undefined = $state();
  let boxEl: HTMLElement | undefined = $state();

  // Debounce so we don't fire a query on every keystroke; 2+ chars to search.
  $effect(() => {
    const v = q.trim();
    const id = setTimeout(() => (debounced = v.length >= 2 ? v : ""), 150);
    return () => clearTimeout(id);
  });

  // A live dropdown doesn't need query caching, so fetch directly with
  // cancellation (the latest term wins) rather than going through TanStack.
  let results = $state<SearchResult[]>([]);
  let searching = $state(false);
  $effect(() => {
    const term = debounced;
    if (term.length < 2) {
      results = [];
      searching = false;
      return;
    }
    let cancelled = false;
    searching = true;
    api
      .search(term)
      .then((r) => {
        if (!cancelled) {
          results = r.results.slice(0, 8); // the full /search page shows everything
          searching = false;
        }
      })
      .catch(() => {
        if (!cancelled) {
          results = [];
          searching = false;
        }
      });
    return () => {
      cancelled = true;
    };
  });
  const showDrop = $derived(open && debounced.length >= 2);

  $effect(() => {
    // Keep the active row in range as results change.
    if (active > results.length - 1) active = results.length - 1;
  });

  function goFull() {
    if (!q.trim()) return;
    open = false;
    inputEl?.blur();
    navigate(`/search?q=${encodeURIComponent(q.trim())}`);
  }
  function choose(r: SearchResult | undefined) {
    if (!r) {
      goFull();
      return;
    }
    open = false;
    inputEl?.blur();
    navigate(r.url);
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      open = true;
      active = Math.min(active + 1, results.length - 1);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      active = Math.max(active - 1, -1);
    } else if (e.key === "Enter") {
      e.preventDefault();
      choose(active >= 0 ? results[active] : undefined);
    } else if (e.key === "Escape") {
      if (showDrop) {
        e.preventDefault();
        open = false;
      }
    }
  }

  // Close when focus/click leaves the box.
  $effect(() => {
    function onDocClick(e: MouseEvent) {
      if (boxEl && !boxEl.contains(e.target as Node)) open = false;
    }
    document.addEventListener("click", onDocClick);
    return () => document.removeEventListener("click", onDocClick);
  });
</script>

<div class="sbox" bind:this={boxEl}>
  <input
    bind:this={inputEl}
    bind:value={q}
    onkeydown={onKey}
    onfocus={() => (open = true)}
    oninput={() => (open = true)}
    placeholder="Search tasks & notes…"
    autocomplete="off"
    spellcheck="false"
    role="combobox"
    aria-expanded={showDrop}
    aria-controls="sbox-list"
    aria-activedescendant={active >= 0 ? `sbox-opt-${active}` : undefined}
  />
  {#if showDrop}
    <ul class="sbox__list" id="sbox-list" role="listbox">
      {#if searching && results.length === 0}
        <li class="sbox__msg muted" role="presentation">Searching…</li>
      {:else if results.length === 0}
        <li class="sbox__msg muted" role="presentation">No matches for “{debounced}”</li>
      {:else}
        {#each results as r, i (r.url + i)}
          <li role="presentation">
            <button
              id={`sbox-opt-${i}`}
              role="option"
              aria-selected={i === active}
              class="sbox__item"
              class:active={i === active}
              onmouseenter={() => (active = i)}
              onclick={() => choose(r)}
            >
              {#if r.kind === "task"}<span class="sbox__kind">task</span>{/if}
              <span class="sbox__title">
                {#each highlightParts(r.title, debounced) as p (p.text + p.hit)}{#if p.hit}<mark
                    >{p.text}</mark
                  >{:else}{p.text}{/if}{/each}
              </span>
              {#if r.snippet}
                <span class="sbox__snippet"
                  >{#each highlightParts(r.snippet, debounced) as p (p.text + p.hit)}{#if p.hit}<mark
                      >{p.text}</mark
                    >{:else}{p.text}{/if}{/each}</span
                >
              {/if}
            </button>
          </li>
        {/each}
        <li role="presentation">
          <button class="sbox__item sbox__all" class:active={active === -1 && false} onclick={goFull}>
            Search all results for “{debounced}” →
          </button>
        </li>
      {/if}
    </ul>
  {/if}
</div>

<style>
  .sbox {
    position: relative;
    flex: 1;
    max-width: 420px;
  }
  .sbox input {
    width: 100%;
    padding: 6px 12px;
    background: var(--bg-inset);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg);
    font-size: 0.9rem;
  }
  .sbox input:focus {
    outline: none;
    border-color: var(--accent);
  }
  .sbox__list {
    position: absolute;
    top: calc(100% + 4px);
    left: 0;
    right: 0;
    z-index: 50;
    margin: 0;
    padding: 4px;
    list-style: none;
    max-height: 60vh;
    overflow-y: auto;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.25);
  }
  .sbox__msg {
    padding: 8px 10px;
    font-size: 0.85rem;
  }
  .sbox__item {
    display: flex;
    align-items: baseline;
    gap: 8px;
    flex-wrap: wrap;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    color: var(--fg);
    padding: 6px 10px;
    border-radius: 4px;
    cursor: pointer;
    font-size: 0.88rem;
  }
  .sbox__item.active {
    background: var(--bg-inset);
  }
  .sbox__kind {
    font-size: 0.62rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--accent-2);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0 6px;
  }
  .sbox__title {
    font-weight: 500;
  }
  .sbox__snippet {
    flex-basis: 100%;
    color: var(--muted);
    font-size: 0.8rem;
  }
  .sbox__item mark {
    background: color-mix(in srgb, var(--accent) 30%, transparent);
    color: inherit;
    border-radius: 2px;
  }
  .sbox__all {
    color: var(--accent);
    font-size: 0.82rem;
  }
</style>
