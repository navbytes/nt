<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import { highlightParts as parts } from "../lib/text";
  import Icon from "../lib/Icon.svelte";

  let { q, tag = "" }: { q: string; tag?: string } = $props();

  const searchQ = createQuery({
    queryKey: ["search", q, tag],
    queryFn: () => api.search(q, tag),
    enabled: q.trim().length > 0 || tag.length > 0,
  });

  // A result is a "task" when the server flags it; everything else is a note
  // (Kind "" defaults to note per the wire contract). The badge label/class is
  // derived once per row.
  function kindOf(k?: string): "task" | "note" {
    return k === "task" ? "task" : "note";
  }
</script>

<header class="shead">
  <p class="shead__eyebrow">Search</p>
  {#if tag}
    <h1 class="shead__title">
      <span class="shead__pill shead__pill--tag">#{tag}</span>
    </h1>
    <p class="shead__meta">Notes &amp; tasks tagged <strong>#{tag}</strong></p>
  {:else}
    <h1 class="shead__title shead__title--q">{q}</h1>
    <p class="shead__meta">Results across your notes &amp; tasks</p>
  {/if}
</header>

{#if q.trim().length === 0 && tag.length === 0}
  <div class="empty empty--hero">
    <span class="empty__art empty__art--onboard"><Icon name="search" size={26} /></span>
    <p class="empty__lead">Search your knowledge base</p>
    <p class="muted">Type a query in the bar above, or pick a tag to see everything filed under it.</p>
  </div>
{:else if $searchQ.isPending}
  <p class="muted">Searching…</p>
{:else if $searchQ.error}
  <div class="empty empty--hero">
    <span class="empty__art empty__art--err"><Icon name="warning" size={24} /></span>
    <p class="empty__lead">Search failed</p>
    <p class="muted">Something went wrong reaching the store.</p>
    <button class="btn btn--ghost btn--sm" onclick={() => $searchQ.refetch()}>Try again</button>
  </div>
{:else if $searchQ.data}
  {#if $searchQ.data.results.length}
    <ul class="results">
      {#each $searchQ.data.results as r, i (i)}
        {@const kind = kindOf(r.kind)}
        <li class="result">
          <span class="result__kind result__kind--{kind}">{kind}</span>
          <div class="result__body">
            <a class="result__title" href={r.url}>
              {#each parts(r.title, q) as p}{#if p.hit}<mark>{p.text}</mark>{:else}{p.text}{/if}{/each}
            </a>
            {#if r.path}<span class="result__path">{r.path}</span>{/if}
            {#if r.snippet}
              <p class="result__snippet">
                {#each parts(r.snippet, q) as p}{#if p.hit}<mark>{p.text}</mark>{:else}{p.text}{/if}{/each}
              </p>
            {/if}
          </div>
        </li>
      {/each}
    </ul>
    {#if $searchQ.data.truncated}
      <p class="results__more">Showing the first {$searchQ.data.results.length} matches — narrow your query for more.</p>
    {/if}
  {:else}
    <div class="empty empty--hero">
      <span class="empty__art empty__art--quiet"><Icon name="search" size={24} /></span>
      <p class="empty__lead">No matches</p>
      <p class="muted">Nothing matched {tag ? `#${tag}` : `“${q}”`}. Try a different term or check the spelling.</p>
    </div>
  {/if}
{/if}

<style>
  /* ── editorial header ────────────────────────────────────────────────────── */
  .shead {
    margin-bottom: var(--space-5);
  }
  .shead__eyebrow {
    margin: 0 0 var(--space-1);
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    letter-spacing: var(--tracking-caps);
    text-transform: uppercase;
    color: var(--accent-color);
  }
  .shead__title {
    font-size: var(--text-display-sm);
    letter-spacing: var(--tracking-display);
    margin: 0 0 var(--space-2);
    overflow-wrap: anywhere;
  }
  /* The query echo reads as a quoted term, not a heading shout. */
  .shead__title--q::before {
    content: "“";
    color: var(--muted);
  }
  .shead__title--q::after {
    content: "”";
    color: var(--muted);
  }
  .shead__meta {
    margin: 0;
    font-family: var(--font-mono);
    font-size: var(--text-callout);
    letter-spacing: var(--tracking-caps);
    text-transform: uppercase;
    color: var(--muted);
  }
  .shead__meta strong {
    color: var(--label-secondary);
  }
  /* The tag chip in the title — spectral, serif-scale, no body text on top. */
  .shead__pill--tag {
    display: inline-flex;
    align-items: center;
    font-family: var(--font-ui);
    font-size: 1.6rem;
    font-weight: 600;
    letter-spacing: -0.01em;
    padding: 2px 16px;
    border-radius: 999px;
    color: var(--fg);
    background:
      linear-gradient(
        135deg,
        color-mix(in srgb, var(--spectral-1) 22%, transparent),
        color-mix(in srgb, var(--spectral-3) 22%, transparent)
      ),
      var(--bg-elevated);
    border: 0.5px solid color-mix(in srgb, var(--spectral-2) 50%, transparent);
    box-shadow: var(--shadow-card);
  }

  /* ── results list (keeps <ul>/<li> + <mark>; links reachable by role/name) ── */
  .results {
    list-style: none;
    padding: 0;
    margin: var(--space-5) 0 0;
  }
  .result {
    display: flex;
    align-items: baseline;
    gap: var(--space-3);
    padding: var(--space-4) 0;
    border-bottom: 0.5px solid var(--separator);
  }
  .result:first-child {
    border-top: 0.5px solid var(--separator);
  }
  .result__body {
    flex: 1;
    min-width: 0;
  }
  .result__title {
    font-weight: 600;
    font-size: var(--text-title2);
    color: var(--fg);
    transition: color var(--motion-fast) var(--ease);
  }
  .result__title:hover {
    color: var(--accent-color);
  }
  /* result-type pill — note (spectral), task (accent). Mono microlabel; colour
     carries the kind, never the reading text. */
  .result__kind {
    flex: none;
    align-self: baseline;
    margin-top: 3px;
    min-width: 42px;
    text-align: center;
    font-family: var(--font-mono);
    font-size: var(--text-footnote);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    padding: 2px 7px;
    border-radius: 999px;
  }
  .result__kind--note {
    /* The spectral tint stays as the background, but the 10px uppercase label
       needs an accessible foreground on it (spectral-2 was ~3.38:1). */
    color: var(--accent-on-tint, #0a52b8);
    background: color-mix(in srgb, var(--spectral-2) 14%, transparent);
  }
  .result__kind--task {
    color: var(--accent-color);
    background: var(--accent-tint);
  }
  .result__path {
    display: inline-block;
    margin-top: 3px;
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    color: var(--muted);
  }
  .result__snippet {
    margin: var(--space-2) 0 0;
    font-size: var(--text-body);
    color: var(--label-secondary);
    line-height: var(--leading-prose);
  }
  .results__more {
    margin: var(--space-4) 0 0;
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    color: var(--muted);
  }
  mark {
    background: var(--accent-tint-strong);
    color: inherit;
    border-radius: 2px;
    padding: 0 1px;
  }

  /* empty/error art variants (the shared .empty--hero shell lives in app.css) */
  .empty__art--err {
    color: var(--red);
    background: color-mix(in srgb, var(--red) 13%, transparent);
  }
  .empty__art--quiet {
    color: var(--muted);
    background: var(--fill);
  }
</style>
