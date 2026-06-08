<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";

  const orphansQ = createQuery({ queryKey: ["orphans"], queryFn: api.orphans });
</script>

<h1>Orphans</h1>
<p class="muted">Notes nothing links to — candidates to connect or prune.</p>

{#if $orphansQ.isPending}
  <p class="muted">Loading…</p>
{:else if $orphansQ.data}
  <ul class="rows">
    {#each $orphansQ.data.notes as n (n.url)}
      <li class="row"><a href={n.url}>{n.title}</a><span class="src">{n.path}</span></li>
    {:else}
      <p class="muted">No orphans — everything's connected.</p>
    {/each}
  </ul>
{/if}
