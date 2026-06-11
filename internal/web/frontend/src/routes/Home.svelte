<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import TaskRows from "../lib/TaskRows.svelte";
  import { dayCapacity } from "../lib/capacity";

  let { canEdit }: { canEdit: boolean } = $props();

  const activityQ = createQuery({ queryKey: ["activity", ""], queryFn: () => api.activity() });
  const recent = $derived(($activityQ.data?.days ?? []).slice(0, 1));

  // "Plan my day" capacity: sum est: across today's plan (overdue + due-today)
  // against the configured daily budget, so you can see at a glance whether
  // you've over-committed. Reuses the same ["tasks"] cache TaskRows fills.
  const tasksQ = createQuery({ queryKey: ["tasks"], queryFn: api.tasks });
  const stateQ = createQuery({ queryKey: ["state"], queryFn: api.state });
  const budget = $derived($stateQ.data?.dayBudgetMin ?? 360);
  const todayGroups = $derived(
    ($tasksQ.data?.groups ?? []).flatMap((g) => {
      // The dashboard plans the same buckets the cockpit shows: overdue + today.
      const rows = g.tasks.filter((t) => {
        if (t.status === "done" || !t.due) return false;
        const d = t.due.slice(0, 10);
        const today = new Date();
        const iso = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, "0")}-${String(today.getDate()).padStart(2, "0")}`;
        return d <= iso;
      });
      return rows.length ? [{ status: g.status, tasks: rows }] : [];
    }),
  );
  const cap = $derived(dayCapacity(todayGroups, budget));
</script>

<h1>Today</h1>

{#if cap.plannedMin > 0 || cap.unestimated > 0}
  <div class="cap" class:cap--over={cap.over} title="Time estimated (est:) for overdue + due-today tasks vs your daily budget">
    <div class="cap__bar"><div class="cap__fill" style:width={`${cap.fraction * 100}%`}></div></div>
    <span class="cap__label">{cap.over ? "⚠ " : ""}{cap.label}</span>
  </div>
{/if}

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
  .cap {
    display: flex;
    align-items: center;
    gap: 10px;
    margin: -4px 0 16px;
  }
  .cap__bar {
    flex: 1;
    max-width: 320px;
    height: 6px;
    background: var(--bg-inset);
    border-radius: 999px;
    overflow: hidden;
  }
  .cap__fill {
    height: 100%;
    background: var(--accent);
    border-radius: 999px;
    transition: width 0.2s;
  }
  .cap--over .cap__fill {
    background: var(--red);
  }
  .cap__label {
    font-size: 0.78rem;
    color: var(--muted);
    white-space: nowrap;
  }
  .cap--over .cap__label {
    color: var(--red);
  }
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
