<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";

  let { q, tag = "" }: { q: string; tag?: string } = $props();

  const searchQ = createQuery({
    queryKey: ["search", q, tag],
    queryFn: () => api.search(q, tag),
    enabled: q.trim().length > 0 || tag.length > 0,
  });

  // Split text around case-insensitive matches of the query so they can be
  // wrapped in <mark> — built as parts (not innerHTML) to stay XSS-safe.
  function parts(text: string, query: string): { text: string; hit: boolean }[] {
    const qq = query.trim();
    if (!qq) return [{ text, hit: false }];
    const lower = text.toLowerCase();
    const ql = qq.toLowerCase();
    const out: { text: string; hit: boolean }[] = [];
    let i = 0;
    while (i < text.length) {
      const idx = lower.indexOf(ql, i);
      if (idx < 0) {
        out.push({ text: text.slice(i), hit: false });
        break;
      }
      if (idx > i) out.push({ text: text.slice(i, idx), hit: false });
      out.push({ text: text.slice(idx, idx + qq.length), hit: true });
      i = idx + qq.length;
    }
    return out;
  }
</script>

<h1>Search</h1>
{#if tag}
  <p class="muted">Notes tagged <a class="tagchip" href={`/search?tag=${encodeURIComponent(tag)}`}>#{tag}</a></p>
{:else}
  <p class="muted">Results for <strong>{q}</strong></p>
{/if}

{#if q.trim().length === 0 && tag.length === 0}
  <p class="muted">Type a query above, or pick a tag.</p>
{:else if $searchQ.isPending}
  <p class="muted">Searching…</p>
{:else if $searchQ.data}
  <ul class="results">
    {#each $searchQ.data.results as r (r.url)}
      <li class="result">
        <a class="result__title" href={r.url}>
          {#each parts(r.title, q) as p}{#if p.hit}<mark>{p.text}</mark>{:else}{p.text}{/if}{/each}
        </a>
        <span class="result__path">{r.path}</span>
        {#if r.snippet}
          <p class="result__snippet">
            {#each parts(r.snippet, q) as p}{#if p.hit}<mark>{p.text}</mark>{:else}{p.text}{/if}{/each}
          </p>
        {/if}
      </li>
    {:else}
      <p class="muted">No matches.</p>
    {/each}
  </ul>
{/if}

<style>
  .results {
    list-style: none;
    padding: 0;
    margin: 16px 0 0;
  }
  .result {
    padding: 10px 0;
    border-bottom: 1px solid var(--border-soft);
  }
  .result__title {
    font-weight: 600;
    font-size: 1rem;
  }
  .result__path {
    margin-left: 10px;
    font-family: var(--font-mono);
    font-size: 0.72rem;
    color: var(--muted);
  }
  .result__snippet {
    margin: 4px 0 0;
    font-size: 0.85rem;
    color: var(--fg-soft);
    line-height: 1.5;
  }
  mark {
    background: color-mix(in srgb, var(--accent) 30%, transparent);
    color: inherit;
    border-radius: 2px;
    padding: 0 1px;
  }
</style>
