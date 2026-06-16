<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import Icon from "../lib/Icon.svelte";

  const tagsQ = createQuery({ queryKey: ["tags"], queryFn: api.tags });

  // Size + emphasis by usage: map each tag's count onto a calm 0..1 weight via a
  // sqrt scale (so a couple of huge tags don't dwarf the long tail into dust),
  // then derive font-size, the spectral tint strength, and a hairline weight from
  // it. Real data only — a single-count vocabulary just renders evenly.
  const tags = $derived($tagsQ.data?.tags ?? []);
  const maxCount = $derived(Math.max(1, ...tags.map((t) => t.count)));
  const total = $derived(tags.reduce((n, t) => n + t.count, 0));

  // weight ∈ 0..1 — sqrt-normalised against the busiest tag.
  function weight(count: number): number {
    return Math.sqrt(count) / Math.sqrt(maxCount);
  }
  // 0.82rem → 1.4rem across the range; bigger tags read as denser hubs.
  function fontSize(count: number): string {
    return (0.82 + weight(count) * 0.58).toFixed(3) + "rem";
  }
  // Spectral wash intensity (chip fill) + border alpha scale with weight so the
  // heavily-used tags glow a touch warmer without ever carrying body text.
  function tintPct(count: number): number {
    return Math.round(10 + weight(count) * 26); // 10%..36%
  }
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
    <div class="tagcloud">
      {#each tags as t (t.name)}
        <a
          class="tag"
          href={`/search?tag=${encodeURIComponent(t.name)}`}
          style="--tag-size:{fontSize(t.count)}; --tag-tint:{tintPct(t.count)}%;"
          title={`${t.count} ${t.count === 1 ? "use" : "uses"} of #${t.name}`}
        >
          <span class="tag__hash">#</span><span class="tag__name">{t.name}</span>
          <span class="tag__n">{t.count}</span>
        </a>
      {/each}
    </div>
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

  /* ── tag cloud: spectral-tinted chips, weighted by usage ─────────────────── */
  .tagcloud {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-3);
  }
  /* A glass-ish chip washed with a spectral tint whose strength tracks usage
     (--tag-tint). The label stays --fg for AA; the spectral only ever colours
     fill + hairline, never the reading text. */
  .tag {
    display: inline-flex;
    align-items: baseline;
    gap: 4px;
    padding: 5px 12px;
    border-radius: 999px;
    font-size: var(--tag-size, 0.95rem);
    line-height: 1.2;
    color: var(--fg);
    background:
      linear-gradient(
        135deg,
        color-mix(in srgb, var(--spectral-1) var(--tag-tint, 16%), transparent),
        color-mix(in srgb, var(--spectral-3) var(--tag-tint, 16%), transparent)
      ),
      var(--bg-elevated);
    border: 0.5px solid color-mix(in srgb, var(--spectral-2) 40%, var(--separator));
    box-shadow: var(--shadow-card);
    transition:
      transform var(--motion-fast) var(--ease),
      box-shadow var(--motion) var(--ease),
      border-color var(--motion-fast) var(--ease);
  }
  .tag:hover {
    text-decoration: none;
    transform: translateY(-1px);
    border-color: color-mix(in srgb, var(--spectral-2) 70%, transparent);
    box-shadow: var(--shadow-card), var(--glow-spectral);
  }
  .tag:active {
    transform: translateY(0) scale(0.98);
  }
  .tag__hash {
    color: var(--spectral-2);
    font-weight: 600;
    opacity: 0.9;
  }
  .tag__name {
    font-weight: 550;
  }
  .tag__n {
    font-family: var(--font-mono);
    font-size: 0.72em;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }
</style>
