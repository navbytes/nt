<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api, type ActivityDay } from "../lib/api";

  let source = $state("");
  const activityQ = createQuery({ queryKey: ["activity", ""], queryFn: () => api.activity() });

  const sources = $derived($activityQ.data?.sources ?? []);
  const days = $derived(filterDays($activityQ.data?.days ?? [], source));

  function filterDays(all: ActivityDay[], src: string): ActivityDay[] {
    if (!src) return all;
    return all
      .map((d) => ({ date: d.date, events: d.events.filter((e) => e.source === src) }))
      .filter((d) => d.events.length > 0);
  }
</script>

<div class="pagehead">
  <h1>Activity</h1>
  <select bind:value={source} class="select">
    <option value="">all sources</option>
    {#each sources as s (s)}<option value={s}>{s}</option>{/each}
  </select>
</div>

{#if $activityQ.isPending}
  <p class="muted">Loading…</p>
{:else}
  {#each days as day (day.date)}
    <div class="actday">{day.date}</div>
    <ul class="rows">
      {#each day.events as ev (ev.when + ev.title)}
        <li class="actrow">
          <span class="act act--{ev.action}">{ev.action}</span>
          <span class="kind">{ev.kind}</span>
          {#if ev.url}<a href={ev.url}>{ev.title}</a>{:else}<span>{ev.title}</span>{/if}
          <span class="src">{ev.source}</span>
        </li>
      {/each}
    </ul>
  {:else}
    <p class="muted">No activity.</p>
  {/each}
{/if}
