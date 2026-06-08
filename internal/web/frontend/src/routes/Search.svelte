<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";

  let { q }: { q: string } = $props();

  const searchQ = createQuery({
    queryKey: ["search", q],
    queryFn: () => api.search(q),
    enabled: q.trim().length > 0,
  });
</script>

<h1>Search</h1>
<p class="muted">Results for <strong>{q}</strong></p>

{#if q.trim().length === 0}
  <p class="muted">Type a query above.</p>
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
