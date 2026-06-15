<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import TaskRows from "../lib/TaskRows.svelte";
  import { dayCapacity } from "../lib/capacity";
  import Icon from "../lib/Icon.svelte";

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

  // A mono "dateline" under the title — small editorial/at-a-glance touch.
  const now = new Date();
  const dateline =
    now.toLocaleDateString(undefined, { weekday: "long" }) +
    " · " +
    now.toLocaleDateString(undefined, { month: "long", day: "numeric" });
</script>

<div class="hero">
  <h1>Today</h1>
  <p class="hero__date">{dateline}</p>
</div>

{#if cap.plannedMin > 0}
  <div class="cap" class:cap--over={cap.over} title="Time estimated (est:) for overdue + due-today tasks vs your daily budget">
    <div class="cap__bar"><div class="cap__fill" style:width={`${cap.fraction * 100}%`}></div></div>
    <span class="cap__label">{#if cap.over}<Icon name="warning" size={13} /> {/if}{cap.label}</span>
  </div>
{:else if cap.unestimated > 0}
  <p class="cap__hint">
    {cap.unestimated} task{cap.unestimated === 1 ? "" : "s"} due today {cap.unestimated === 1
      ? "has"
      : "have"} no time estimate
  </p>
{/if}

<div class="dash">
  <div class="dash__main">
    <!-- The daily cockpit: what needs attention now (overdue + due today) plus a
         capture box. The full backlog and other views live under Tasks. -->
    <TaskRows
      view="agenda"
      buckets={["Overdue", "Today"]}
      showAdd={true}
      emptyText="Nothing overdue or due today — you're all clear."
    />
  </div>

  <aside class="dash__side">
    <div class="dash__side-head">
      <h2 class="group__title">Recent activity</h2>
      <a class="dash__all" href="/activity">View all →</a>
    </div>
    {#each recent as day (day.date)}
      <div class="actday">{day.date}</div>
      <ul class="feed">
        {#each day.events.slice(0, 12) as ev (ev.when + ev.title)}
          <li class="feed__item">
            <span class="feed__dot feed__dot--{ev.action}" title={ev.action} aria-hidden="true"></span>
            {#if ev.url}<a class="feed__title" href={ev.url}>{ev.title}</a>{:else}<span class="feed__title">{ev.title}</span>{/if}
          </li>
        {/each}
      </ul>
    {:else}
      <p class="muted small">No activity yet.</p>
    {/each}
  </aside>
</div>

<style>
  .hero {
    margin-bottom: 16px;
  }
  .hero h1 {
    margin-bottom: 3px;
  }
  .hero__date {
    margin: 0;
    font-family: var(--font-mono);
    font-size: 0.72rem;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--muted);
  }
  .cap {
    display: flex;
    align-items: center;
    gap: 12px;
    margin: -4px 0 18px;
  }
  .cap__bar {
    flex: 1;
    max-width: 360px;
    height: 8px;
    background: var(--fill);
    border-radius: 999px;
    overflow: hidden;
    box-shadow: inset 0 0 0 0.5px var(--separator);
  }
  .cap__fill {
    position: relative;
    overflow: hidden;
    height: 100%;
    /* vivid signature gradient + indigo glow — a lively, modern day-progress
       moment, with a one-time shimmer sweep as it fills. */
    background: var(--grad-vivid);
    border-radius: 999px;
    box-shadow: 0 0 12px -1px rgba(95, 64, 224, 0.5);
    transition: width var(--motion-slow) var(--ease-out);
  }
  .cap__fill::after {
    content: "";
    position: absolute;
    inset: 0;
    background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.45), transparent);
    transform: translateX(-120%);
    animation: cap-shimmer 1.5s var(--ease-out) 0.25s both;
  }
  @keyframes cap-shimmer {
    to {
      transform: translateX(120%);
    }
  }
  .cap--over .cap__fill {
    background: linear-gradient(90deg, var(--red), color-mix(in srgb, var(--red) 65%, #ff8c00));
    box-shadow: 0 0 8px -1px color-mix(in srgb, var(--red) 55%, transparent);
  }
  .cap__label {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 0.78rem;
    color: var(--muted);
    white-space: nowrap;
  }
  .cap--over .cap__label {
    color: var(--red);
  }
  .cap__hint {
    margin: -4px 0 16px;
    font-size: 0.8rem;
    color: var(--muted);
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

  /* Recent-activity feed: a clean hairline-divided list (no stretched pills,
     no per-row source noise). A small status dot encodes the action; the title
     stays in label-secondary so blue stays reserved for actions. */
  .feed {
    list-style: none;
    margin: 6px 0 0;
    padding: 0;
  }
  .feed__item {
    display: flex;
    align-items: baseline;
    gap: 9px;
    padding: 7px 0;
    border-bottom: 0.5px solid var(--separator);
    min-width: 0;
  }
  .feed__item:last-child {
    border-bottom: 0;
  }
  .feed__dot {
    flex: none;
    align-self: center;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--muted);
  }
  .feed__dot--added {
    background: var(--green);
  }
  .feed__dot--completed {
    background: var(--accent-color);
  }
  .feed__dot--updated {
    background: var(--teal);
  }
  .feed__title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--text-body);
    color: var(--fg-soft);
    transition: color var(--motion-fast) var(--ease);
  }
  a.feed__title:hover {
    color: var(--accent-color);
    text-decoration: none;
  }
</style>
