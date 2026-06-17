<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import Icon from "../lib/Icon.svelte";

  type Tag = { name: string; count: number };
  type Sort = "count" | "alpha";

  const tagsQ = createQuery({ queryKey: ["tags"], queryFn: api.tags });

  const tags = $derived<Tag[]>($tagsQ.data?.tags ?? []);
  // Busiest tag drives every usage bar's width; floor at 1 to avoid /0.
  const maxCount = $derived(Math.max(1, ...tags.map((t) => t.count)));
  const total = $derived(tags.reduce((n, t) => n + t.count, 0));

  // ── toolbar state: live name filter + sort order ─────────────────────────
  let filter = $state("");
  let sort = $state<Sort>("count");

  // Pure helper: case-insensitive substring filter, then sort. "count" sorts by
  // usage desc with an alphabetical tie-break; "alpha" sorts by name.
  function arrange(list: Tag[], q: string, by: Sort): Tag[] {
    const needle = q.trim().toLowerCase();
    const out = needle ? list.filter((t) => t.name.toLowerCase().includes(needle)) : list.slice();
    out.sort((a, b) =>
      by === "alpha"
        ? a.name.localeCompare(b.name)
        : b.count - a.count || a.name.localeCompare(b.name),
    );
    return out;
  }

  const shown = $derived(arrange(tags, filter, sort));
  const filtering = $derived(filter.trim().length > 0);
</script>

<header class="thead">
  <p class="thead__eyebrow">Vocabulary</p>
  <h1 class="thead__title">Tags</h1>
  {#if tags.length}
    <p class="thead__meta">{tags.length} {tags.length === 1 ? "tag" : "tags"} · {total} uses</p>
  {/if}
</header>

{#if $tagsQ.isPending}
  <p class="muted">Loading…</p>
{:else if $tagsQ.error}
  <div class="empty">
    <p class="empty__lead">Couldn't load tags</p>
    <button class="btn btn--ghost btn--sm" onclick={() => $tagsQ.refetch()}>Try again</button>
  </div>
{:else if $tagsQ.data}
  {#if tags.length}
    <!-- ── toolbar: live name filter + sort toggle ──────────────────────── -->
    <div class="toolbar">
      <div class="qfilter">
        <Icon name="search" size={15} />
        <input
          type="text"
          placeholder="Filter tags…"
          aria-label="Filter tags by name"
          bind:value={filter}
        />
        {#if filtering}
          <button class="qfilter__clear" aria-label="Clear filter" onclick={() => (filter = "")}>
            <Icon name="close" size={13} />
          </button>
        {/if}
      </div>

      <div class="toolbar__right">
        {#if filtering}
          <span class="toolbar__count" aria-live="polite">{shown.length} of {tags.length}</span>
        {/if}
        <div class="seg" role="group" aria-label="Sort tags">
          <button class:seg--on={sort === "count"} aria-pressed={sort === "count"} onclick={() => (sort = "count")}>
            Most used
          </button>
          <button class:seg--on={sort === "alpha"} aria-pressed={sort === "alpha"} onclick={() => (sort = "alpha")}>
            A–Z
          </button>
        </div>
      </div>
    </div>

    {#if shown.length}
      <ul class="taglist">
        {#each shown as t (t.name)}
          <li>
            <a
              class="tagitem"
              href={`/search?tag=${encodeURIComponent(t.name)}`}
              style="--bar:{Math.round((t.count / maxCount) * 100)}%;"
              title={`${t.count} ${t.count === 1 ? "use" : "uses"} of #${t.name}`}
            >
              <span class="tagitem__name"><span class="tagitem__hash">#</span>{t.name}</span>
              <span class="tagitem__n">{t.count}</span>
              <span class="tagitem__bar" aria-hidden="true"></span>
            </a>
          </li>
        {/each}
      </ul>
    {:else}
      <!-- distinct from the no-tags-at-all state: the filter matched nothing -->
      <div class="empty">
        <p class="empty__lead">No tags match “{filter.trim()}”</p>
        <button class="btn btn--ghost btn--sm" onclick={() => (filter = "")}>Clear filter</button>
      </div>
    {/if}
  {:else}
    <div class="empty empty--hero">
      <span class="empty__art empty__art--onboard"><Icon name="tag" size={28} /></span>
      <p class="empty__lead">No tags yet</p>
      <p class="muted">Add <code>#a-tag</code> to a task or note and it'll show up here.</p>
    </div>
  {/if}
{/if}

<style>
  /* ── editorial page header (matches the Today / dashboard hero language) ── */
  .thead {
    margin-bottom: var(--space-6);
  }
  .thead__eyebrow {
    margin: 0 0 var(--space-1);
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    letter-spacing: var(--tracking-caps);
    text-transform: uppercase;
    color: var(--accent-color);
  }
  .thead__title {
    font-size: var(--text-display-sm);
    letter-spacing: var(--tracking-display);
    margin: 0 0 var(--space-2);
  }
  .thead__meta {
    margin: 0;
    font-family: var(--font-mono);
    font-size: var(--text-callout);
    letter-spacing: var(--tracking-caps);
    text-transform: uppercase;
    color: var(--muted);
  }

  /* ── toolbar: filter field + result count + sort segmented control ───────── */
  .toolbar {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    flex-wrap: wrap;
    margin-bottom: var(--space-5);
  }
  .toolbar__right {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    margin-left: auto;
  }
  .toolbar__count {
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    letter-spacing: var(--tracking-caps);
    text-transform: uppercase;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  /* Glass text field with a leading search glyph (matches the app's filter
     inputs). The label/value stay on --fg for AA in both themes. */
  .qfilter {
    position: relative;
    display: flex;
    align-items: center;
    flex: 1 1 240px;
    min-width: 200px;
  }
  .qfilter :global(.icon) {
    position: absolute;
    left: 10px;
    color: var(--muted);
    pointer-events: none;
  }
  .qfilter input {
    width: 100%;
    padding: 7px 28px 7px 32px;
    font: inherit;
    font-size: var(--text-body);
    background: color-mix(in srgb, var(--bg-elevated) 70%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    border: 0.5px solid var(--separator);
    box-shadow: var(--glass-hairline);
    border-radius: var(--radius-sm);
    color: var(--fg);
    transition:
      border-color var(--motion-fast) var(--ease),
      box-shadow var(--motion-fast) var(--ease);
  }
  .qfilter input:focus {
    outline: none;
    border-color: var(--accent-color);
    box-shadow: var(--focus-ring-tight);
  }
  .qfilter__clear {
    position: absolute;
    right: 6px;
    top: 50%;
    transform: translateY(-50%);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: none;
    border: none;
    color: var(--muted);
    cursor: pointer;
    line-height: 1;
    padding: 2px 6px;
    border-radius: var(--radius-xs);
    transition: color var(--motion-fast) var(--ease);
  }
  .qfilter__clear:hover {
    color: var(--fg);
  }

  /* Glass segmented control: a translucent track holding the active pill, with a
     short spectral underline. Mono microlabels in caps. Text stays AA on --fg. */
  .seg {
    display: inline-flex;
    gap: 2px;
    padding: 2px;
    border-radius: var(--radius-sm);
    background: color-mix(in srgb, var(--bg-elevated) 70%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    border: 0.5px solid var(--separator);
    box-shadow: var(--glass-hairline);
  }
  .seg button {
    position: relative;
    border: none;
    background: none;
    cursor: pointer;
    padding: 4px 12px;
    border-radius: var(--radius-xs);
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    color: var(--muted);
    white-space: nowrap;
    transition:
      color var(--motion-fast) var(--ease),
      background var(--motion-fast) var(--ease);
  }
  .seg button:hover:not(.seg--on) {
    color: var(--fg);
  }
  .seg--on {
    color: var(--fg);
    background: var(--bg-elevated);
    box-shadow: var(--glass-hairline);
  }
  /* Short spectral underline anchoring the active segment (decorative). */
  .seg--on::after {
    content: "";
    position: absolute;
    left: 50%;
    bottom: 2px;
    transform: translateX(-50%);
    width: 16px;
    height: 1.5px;
    border-radius: 999px;
    background: var(--grad-spectral);
  }

  /* ── scannable tag grid: each item is a link with #name, mono count, and a
        subtle usage bar (width = count / maxCount, spectral fill, non-text). ── */
  .taglist {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
    gap: var(--space-3);
  }
  .taglist li {
    min-width: 0;
  }
  .tagitem {
    position: relative;
    display: grid;
    grid-template-columns: 1fr auto;
    align-items: baseline;
    gap: var(--space-3);
    overflow: hidden;
    padding: 10px 14px 12px;
    border-radius: var(--radius-md);
    color: var(--fg);
    background: color-mix(in srgb, var(--bg-elevated) 82%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    border: 0.5px solid var(--separator);
    box-shadow: var(--shadow-card);
    transition:
      transform var(--motion-fast) var(--ease),
      box-shadow var(--motion) var(--ease),
      border-color var(--motion-fast) var(--ease);
  }
  .tagitem:hover {
    text-decoration: none;
    transform: translateY(-1px);
    border-color: color-mix(in srgb, var(--spectral-2) 70%, transparent);
    box-shadow: var(--shadow-card), var(--glow-spectral);
  }
  .tagitem:active {
    transform: translateY(0) scale(0.99);
  }
  .tagitem__name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 550;
  }
  .tagitem__hash {
    color: var(--spectral-2);
    font-weight: 600;
    opacity: 0.9;
  }
  .tagitem__n {
    flex: none;
    font-family: var(--font-mono);
    font-size: 0.82em;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }
  /* Usage bar: a thin spectral fill pinned to the bottom edge, width tracks
     count/maxCount via --bar. Purely decorative (aria-hidden). */
  .tagitem__bar {
    grid-column: 1 / -1;
    position: absolute;
    left: 0;
    bottom: 0;
    height: 3px;
    width: var(--bar, 0%);
    border-radius: 0 999px 999px 0;
    background: var(--grad-spectral);
    opacity: 0.75;
  }
</style>
