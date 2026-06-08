<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import TaskRows from "../lib/TaskRows.svelte";

  let { canEdit }: { canEdit: boolean } = $props();

  const activityQ = createQuery({ queryKey: ["activity", ""], queryFn: () => api.activity() });
  const recent = $derived(($activityQ.data?.days ?? []).slice(0, 1));
</script>

<h1>Dashboard</h1>

<div class="dash">
  <div class="dash__main">
    <TaskRows {canEdit} statuses={["doing", "blocked", "open"]} showAdd={true} />
  </div>

  <aside class="dash__side">
    <h2 class="group__title">Recent activity</h2>
    {#each recent as day (day.date)}
      <div class="actday">{day.date}</div>
      <ul class="rows">
        {#each day.events.slice(0, 12) as ev (ev.when + ev.title)}
          <li class="actrow">
            <span class="act act--{ev.action}">{ev.action}</span>
            {#if ev.url}<a href={ev.url}>{ev.title}</a>{:else}<span>{ev.title}</span>{/if}
            <span class="src">{ev.source}</span>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="muted small">No activity yet.</p>
    {/each}
  </aside>
</div>
