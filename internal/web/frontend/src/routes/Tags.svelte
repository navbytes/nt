<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";

  const tagsQ = createQuery({ queryKey: ["tags"], queryFn: api.tags });
</script>

<h1>Tags</h1>

{#if $tagsQ.isPending}
  <p class="muted">Loading…</p>
{:else if $tagsQ.data}
  <div class="tagcloud">
    {#each $tagsQ.data.tags as t (t.name)}
      <a class="tagchip" href={`/search?tag=${encodeURIComponent(t.name)}`}>
        #{t.name}<span class="tagchip__n">{t.count}</span>
      </a>
    {:else}
      <p class="muted">No tags yet.</p>
    {/each}
  </div>
{/if}
