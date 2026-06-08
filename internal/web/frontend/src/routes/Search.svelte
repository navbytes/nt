<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";

  let { q, tag = "" }: { q: string; tag?: string } = $props();

  const searchQ = createQuery({
    queryKey: ["search", q, tag],
    queryFn: () => api.search(q, tag),
    enabled: q.trim().length > 0 || tag.length > 0,
  });
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
  <ul class="rows">
    {#each $searchQ.data.results as r (r.url)}
      <li class="row"><a href={r.url}>{r.title}</a><span class="src">{r.path}</span></li>
    {:else}
      <p class="muted">No matches.</p>
    {/each}
  </ul>
{/if}
