<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import TaskRows from "../lib/TaskRows.svelte";

  let { canEdit }: { canEdit: boolean } = $props();

  const activityQ = createQuery({ queryKey: ["activity", ""], queryFn: () => api.activity() });
  const recent = $derived(($activityQ.data?.days ?? []).slice(0, 1));
</script>

<h1>Today</h1>

<div class="dash">
  <div class="dash__main">
    <!-- The daily cockpit: what needs attention now (overdue + due today) plus a
         capture box. The full backlog and other views live under Tasks. -->
    <TaskRows
      {canEdit}
      view="agenda"
      buckets={["Overdue", "Today"]}
      showAdd={true}
      emptyText="Nothing overdue or due today — you're clear ✨"
    />
  </div>

  <aside class="dash__side">
    <div class="dash__side-head">
      <h2 class="group__title">Recent activity</h2>
      <a class="dash__all" href="/activity">View all →</a>
    </div>
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

<style>
  .dash__side-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
  }
  .dash__all {
    font-size: 0.78rem;
    color: var(--muted);
  }
  .dash__all:hover {
    color: var(--accent);
  }
</style>
