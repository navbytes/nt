<script lang="ts">
  // The top-bar search: results-as-you-type in a dropdown, with the matched
  // query highlighted. Enter (or a click) opens the active result; Enter with no
  // active row opens the full /search page. Reuses the same ranked search API and
  // highlight helper the /search route uses.
  import { api } from "./api";
  import { useQueryClient } from "@tanstack/svelte-query";
  import { navigate } from "./router.svelte";
  import { highlightParts } from "./text";
  import { parseQuickAdd } from "./quickparse";
  import { showToast } from "./toast.svelte";
  import type { SearchResult } from "./api-types";
  import Icon from "./Icon.svelte";

  const qc = useQueryClient();
  let q = $state("");
  let adding = $state(false);
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

  // The search field and the on-page quick-add look alike, so people type a task
  // into search by mistake and hit "No matches". Parse the query with the same
  // grammar the quick-add uses so we can offer to capture it as a task instead —
  // emphasised when it carries todo.txt syntax (due:/!pri/@tag/+project).
  const parsed = $derived(debounced.length >= 2 ? parseQuickAdd(debounced) : null);
  const taskLike = $derived(
    !!parsed && (!!parsed.priority || !!parsed.due || !!parsed.start || !!parsed.project || parsed.tags.length > 0),
  );

  async function addAsTask() {
    const text = q.trim();
    if (!text || adding) return;
    adding = true;
    try {
      await api.taskNew(text);
      await qc.invalidateQueries({ queryKey: ["tasks"] });
      qc.invalidateQueries({ queryKey: ["state"] });
      showToast(`Added task: ${parsed?.title || text}`);
      open = false;
      q = "";
      active = -1;
      inputEl?.blur();
      navigate("/tasks");
    } catch (e) {
      showToast(`Couldn't add task: ${String(e)}`);
    } finally {
      adding = false;
    }
  }

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
      // ⌘/Ctrl+Enter always captures the query as a task, from any row.
      if (e.metaKey || e.ctrlKey) {
        addAsTask();
      } else if (active >= 0) {
        choose(results[active]);
      } else if (results.length === 0 && debounced.length >= 2) {
        // Nothing to open — capture the query as a task instead of opening an
        // empty search page. Rescues a task typed into search by mistake.
        addAsTask();
      } else {
        goFull();
      }
    } else if (e.key === "Escape") {
      // Esc: dismiss the dropdown if open, otherwise clear the field (a second
      // press, or Esc on an already-closed list, empties it — NSSearchField feel).
      e.preventDefault();
      if (showDrop) {
        open = false;
      } else if (q) {
        q = "";
        active = -1;
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
  <Icon name="search" size={14} />
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
      {:else}
        {#if results.length === 0}
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
          <button class="sbox__item sbox__all" onclick={goFull}>
            Search all results for “{debounced}”
            <Icon name="arrow-right" size={13} />
          </button>
        </li>
        {/if}
        <!-- Rescue: capture the query as a task (the field is easy to confuse
             with the on-page quick-add). Emphasised when it carries task syntax. -->
        <li role="presentation">
          <button class="sbox__item sbox__add" class:sbox__add--strong={taskLike} onclick={addAsTask} disabled={adding}>
            <Icon name="plus" size={13} />
            <span class="sbox__addlabel">Add <strong>“{parsed?.title || debounced}”</strong> as a task</span>
            {#if taskLike}
              <span class="sbox__addmeta">{#if parsed?.priority}!{parsed.priority} {/if}{#if parsed?.due}due:{parsed.due} {/if}{#if parsed?.project}+{parsed.project} {/if}{#each parsed?.tags ?? [] as t}@{t} {/each}</span>
            {/if}
            <span class="sbox__kbd">⌘↵</span>
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
  /* Leading magnifier: a real <Icon> so it inherits currentColor and stays
     visible in both themes (finding 9 — the old data-URI baked in #6e6e73,
     near-invisible on dark). Positioned absolutely over the padded input. */
  .sbox :global(.icon) {
    position: absolute;
    left: 11px;
    top: 50%;
    transform: translateY(-50%);
    color: var(--muted);
    pointer-events: none;
  }
  /* NSSearchField: a fully-rounded, glassy pill with a leading magnifier.
     backdrop-filter lets the toolbar vibrancy show through the fill. */
  .sbox input {
    width: 100%;
    padding: 6px 14px 6px 32px;
    background-color: color-mix(in srgb, var(--fill) 80%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(8px);
    backdrop-filter: saturate(var(--glass-saturate)) blur(8px);
    border: 1px solid var(--control-border); /* finding 6 — input edge is the only affordance */
    border-radius: 999px;
    color: var(--fg);
    font-size: 0.9rem;
    transition:
      border-color var(--motion-fast) var(--ease),
      background-color var(--motion-fast) var(--ease),
      box-shadow var(--motion-fast) var(--ease);
  }
  .sbox input:hover {
    background-color: var(--fill);
  }
  /* Drop the bare outline:none so the global :focus-visible halo shows; on focus
     swap to the accent border + a faint spectral ring (mouse focus too). */
  .sbox input:focus {
    border-color: var(--accent);
  }
  .sbox input:focus-visible {
    background-color: var(--bg-elevated);
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
    background: color-mix(in srgb, var(--bg-elevated) 90%, transparent);
    -webkit-backdrop-filter: blur(20px) saturate(170%);
    backdrop-filter: blur(20px) saturate(170%);
    border: 1px solid var(--control-border); /* finding 6 — dropdown edge is the only affordance */
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-popover);
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
    border-radius: var(--radius-xs); /* finding 16 — was literal 4px */
    cursor: pointer;
    font-size: 0.88rem;
  }
  .sbox__item.active {
    background: var(--accent-fill);
    color: var(--on-accent);
  }
  /* Inside the selected row, secondary text + the kind pill go translucent white
     (matching .palette__item.active) so they stay legible on the accent fill. */
  .sbox__item.active .sbox__snippet {
    color: color-mix(in srgb, var(--on-accent) 75%, transparent);
  }
  .sbox__item.active .sbox__kind {
    color: color-mix(in srgb, var(--on-accent) 80%, transparent);
    border-color: color-mix(in srgb, var(--on-accent) 40%, transparent);
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
    background: var(--accent-tint-strong);
    color: inherit;
    border-radius: 2px;
  }
  /* On the selected (accent-filled) row, the tint would vanish — use a
     translucent white wash so matched spans still stand out. */
  .sbox__item.active mark {
    background: color-mix(in srgb, var(--on-accent) 28%, transparent);
  }
  .sbox__all {
    display: flex;
    align-items: center;
    gap: 5px;
    color: var(--accent);
    font-size: 0.82rem;
  }
  .sbox__item.active.sbox__all {
    color: var(--on-accent);
  }
  /* Capture-as-task rescue row: sits below results, separated by a hairline. */
  .sbox__add {
    display: flex;
    align-items: center;
    gap: 7px;
    color: var(--label-secondary);
    font-size: 0.82rem;
    border-top: 1px solid var(--border-soft);
  }
  .sbox__add--strong {
    color: var(--accent);
  }
  .sbox__add:disabled {
    opacity: 0.55;
  }
  .sbox__addlabel {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sbox__addlabel strong {
    font-weight: 600;
  }
  .sbox__addmeta {
    font-family: var(--font-mono);
    font-size: 0.72rem;
    color: var(--accent-2);
    white-space: nowrap;
  }
  .sbox__kbd {
    margin-left: auto;
    font-size: 0.7rem;
    color: var(--muted);
    border: 1px solid var(--border);
    border-radius: 5px;
    padding: 0 5px;
    white-space: nowrap;
  }
  .sbox__item.active .sbox__kbd {
    color: var(--on-accent);
    border-color: color-mix(in srgb, var(--on-accent) 40%, transparent);
  }
</style>
