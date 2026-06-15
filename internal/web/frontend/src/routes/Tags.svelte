<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";

  const tagsQ = createQuery({ queryKey: ["tags"], queryFn: api.tags });
</script>

<h1>Tags</h1>

{#if $tagsQ.isPending}
  <p class="muted">Loading…</p>
{:else if $tagsQ.error}
  <div class="empty">
    <p class="empty__lead">Couldn't load tags</p>
    <button class="btn btn--ghost btn--sm" onclick={() => $tagsQ.refetch()}>Try again</button>
  </div>
{:else if $tagsQ.data}
  {#if $tagsQ.data.tags.length}
    <div class="tagcloud">
      {#each $tagsQ.data.tags as t (t.name)}
        <a class="tagchip" href={`/search?tag=${encodeURIComponent(t.name)}`}>
          #{t.name}<span class="tagchip__n">{t.count}</span>
        </a>
      {/each}
    </div>
  {:else}
    <div class="empty">
      <p class="empty__lead">No tags yet</p>
      <p class="muted">Add <code>#a-tag</code> to a task or note and it'll show up here.</p>
    </div>
  {/if}
{/if}
