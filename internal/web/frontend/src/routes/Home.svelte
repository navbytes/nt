<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import type { ActivityEvent } from "../lib/api-types";
  import TaskRows from "../lib/TaskRows.svelte";
  import { dayCapacity } from "../lib/capacity";
  import { fmtDuration } from "../lib/text";
  import Icon from "../lib/Icon.svelte";
  import Ring from "../lib/Ring.svelte";
  import Sparkline from "../lib/Sparkline.svelte";

  // ── data sources (all real; nothing here is fabricated) ──────────────────
  const activityQ = createQuery({ queryKey: ["activity", ""], queryFn: () => api.activity() });
  const tasksQ = createQuery({ queryKey: ["tasks"], queryFn: api.tasks });
  const stateQ = createQuery({ queryKey: ["state"], queryFn: api.state });

  // The most recent day of the provenance feed (server groups newest-first).
  const recent = $derived(($activityQ.data?.days ?? []).slice(0, 1));
  // Flat, newest-first event stream (across all days) for the trend + tallies.
  const allEvents = $derived<ActivityEvent[]>(($activityQ.data?.days ?? []).flatMap((d) => d.events));

  const openCount = $derived($stateQ.data?.openCount ?? 0);
  const noteCount = $derived($stateQ.data?.noteCount ?? 0);

  // Activity is conveyed by a dot HUE alone, which fails colour-blind users and
  // screen readers (the verb was hover-only/aria-hidden). Pair each action with a
  // glyph (a second, non-colour cue) and a spoken verb (finding 11).
  function actionIcon(action: string): string {
    if (action === "added" || action === "created") return "plus";
    if (action === "completed") return "check";
    if (action === "archived" || action === "deleted") return "archive";
    return "edit"; // updated / anything else
  }
  function actionVerb(action: string): string {
    if (action === "added") return "Added";
    if (action === "created") return "Created";
    if (action === "completed") return "Completed";
    if (action === "updated") return "Updated";
    if (action === "archived") return "Archived";
    if (action === "deleted") return "Deleted";
    return action ? action[0]!.toUpperCase() + action.slice(1) : "Changed";
  }

  // ── today's plan: overdue + due-today, open — the same buckets the FOCUS
  //    card renders. Reuses the ["tasks"] cache TaskRows fills. ───────────────
  function todayISO(d = new Date()): string {
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
  }
  const ISO_TODAY = todayISO();

  const budget = $derived($stateQ.data?.dayBudgetMin ?? 360);
  const todayGroups = $derived(
    ($tasksQ.data?.groups ?? []).flatMap((g) => {
      const rows = g.tasks.filter((t) => {
        if (t.status === "done" || !t.due) return false;
        return t.due.slice(0, 10) <= ISO_TODAY;
      });
      return rows.length ? [{ status: g.status, tasks: rows }] : [];
    }),
  );
  const cap = $derived(dayCapacity(todayGroups, budget));
  // How many open tasks are actually on today's plate (drives the FOCUS count).
  const focusCount = $derived(todayGroups.reduce((n, g) => n + g.tasks.length, 0));

  // ── momentum, derived from the REAL activity feed ────────────────────────
  // A completion's timestamp is date-only (midnight); compare on the date part.
  const localDate = (when: string): string => {
    const t = new Date(when);
    return Number.isNaN(t.getTime()) ? when.slice(0, 10) : todayISO(t);
  };
  const completedToday = $derived(
    allEvents.filter((e) => e.action === "completed" && localDate(e.when) === ISO_TODAY).length,
  );

  // 7-day completion trend: count "completed" events per local day across the
  // last 7 days (oldest→newest), so the sparkline reads left-to-right like a
  // calendar week. Entirely real — an empty week renders a calm baseline.
  const WEEK = 7;
  const weekDays = $derived.by(() => {
    const days: { iso: string; count: number }[] = [];
    const base = new Date();
    for (let i = WEEK - 1; i >= 0; i--) {
      const d = new Date(base);
      d.setDate(base.getDate() - i);
      days.push({ iso: todayISO(d), count: 0 });
    }
    const idx = new Map(days.map((d, i) => [d.iso, i]));
    for (const e of allEvents) {
      if (e.action !== "completed") continue;
      const i = idx.get(localDate(e.when));
      if (i !== undefined) days[i]!.count++;
    }
    return days;
  });
  const weekSeries = $derived(weekDays.map((d) => d.count));
  const weekTotal = $derived(weekSeries.reduce((a, b) => a + b, 0));
  // Only show the trend tile once there's something real to plot.
  const hasWeekData = $derived(weekTotal > 0);

  // A short relative-day label for a feed timestamp ("Today", "Yesterday",
  // "3d ago", or an absolute date). Honest about the date-only precision of
  // completions — we never invent a clock time.
  function relDay(when: string): string {
    const iso = localDate(when);
    const [y, m, d] = iso.split("-").map(Number);
    if (!y || !m || !d) return "";
    const day = new Date(y, m - 1, d);
    const today = new Date(ISO_TODAY + "T00:00:00");
    const diff = Math.round((day.getTime() - today.getTime()) / 86400000);
    if (diff === 0) return "Today";
    if (diff === -1) return "Yesterday";
    if (diff <= -2 && diff >= -6) return `${-diff}d ago`;
    return day.toLocaleDateString(undefined, { month: "short", day: "numeric" });
  }

  // ── hero dateline (kept from the original) ───────────────────────────────
  const now = new Date();
  const dateline =
    now.toLocaleDateString(undefined, { weekday: "long" }) +
    " · " +
    now.toLocaleDateString(undefined, { month: "long", day: "numeric" });

  // A failed tasks/activity fetch must NOT read as "all clear" (W7) — branch on
  // the error so the hero shows a load-failure state instead of false calm.
  const loadFailed = $derived(!!$tasksQ.error || !!$activityQ.error);
  // The day is "clear" only once tasks have loaded WITHOUT error and nothing is
  // due/overdue.
  const dayClear = $derived(!$tasksQ.isPending && !loadFailed && focusCount === 0);
</script>

<div class="hero">
  <p class="hero__eyebrow">Cockpit</p>
  <h1 class="hero__title">Today</h1>
  <p class="hero__date">{dateline}</p>
</div>

<div class="bento">
  <!-- ── MAIN COLUMN: the prominent Focus card, then the Recent-activity feed
          stacked directly beneath it. A self-contained flex stack, so it never
          shares grid rows with the rail (which is what created the voids). ──── -->
  <div class="bento__col bento__col--main">
  <!-- ── FOCUS (largest): overdue + due-today agenda, rendered by the shared
          TaskRow/TaskRows. Quick Capture is TaskRows' own add form (live parse
          preview); when the plan is empty TaskRows draws the .empty--hero
          "all clear" state itself via emptyText. ───────────────────────────── -->
  <section class="card card--focus" class:card--clear={dayClear} aria-labelledby="focus-h">
    <div class="card__head">
      <h2 class="card__label" id="focus-h">Focus</h2>
      {#if !$tasksQ.isPending && !loadFailed}
        <span class="card__count">{focusCount} {focusCount === 1 ? "task" : "tasks"}</span>
      {/if}
    </div>

    {#if loadFailed}
      <!-- A failed fetch must read as a problem, never as "all clear" (W7). -->
      <div class="loaderr" role="alert">
        <p class="loaderr__msg">Couldn't load your day. Check your connection and retry.</p>
        <button
          class="btn btn--ghost btn--sm"
          onclick={() => {
            $tasksQ.refetch();
            $activityQ.refetch();
          }}>Try again</button
        >
      </div>
    {:else}
      <TaskRows
        view="agenda"
        buckets={["Overdue", "Today"]}
        showAdd={true}
        emptyText="Nothing overdue or due today — you're all clear."
      />

      <p class="card__hint">
        <kbd>c</kbd> to capture · <kbd>⌘K</kbd> for commands
      </p>
    {/if}
  </section>

  <!-- ── RECENT ACTIVITY: existing feed (dots + hairlines), stacked under
          Focus in the main column. ──────────────────────────────────────── -->
  <section class="card card--feed" aria-labelledby="act-h">
    <div class="card__head">
      <h2 class="card__label" id="act-h">Recent activity</h2>
      <a class="card__all" href="/activity">View all <Icon name="arrow-right" size={13} /></a>
    </div>
    {#each recent as day (day.date)}
      <ul class="feed">
        {#each (day.events ?? []).slice(0, 9) as ev (ev.when + ev.title)}
          <li class="feed__item">
            <span class="feed__glyph feed__glyph--{ev.action}" title={actionVerb(ev.action)} aria-hidden="true"><Icon name={actionIcon(ev.action)} size={11} /></span>
            <!-- Spoken verb so the action isn't carried by colour alone (finding 11). -->
            <span class="feed__verb">{actionVerb(ev.action)}:</span>
            {#if ev.url}<a class="feed__title" href={ev.url}>{ev.title}</a>{:else}<span class="feed__title">{ev.title}</span>{/if}
            <span class="feed__when">{relDay(ev.when)}</span>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="card__blank muted">No activity yet — your adds and completions land here.</p>
    {/each}
  </section>
  </div>

  <!-- ── RAIL COLUMN: capacity → momentum → this-week, a 1fr flex stack that
          keeps its own rows independent of the main column. ───────────────── -->
  <div class="bento__col bento__col--rail">
  <!-- ── CAPACITY RING: planned est: vs daily budget (capacity.ts math) ───── -->
  <section class="card card--capacity" aria-labelledby="cap-h">
    <div class="card__head">
      <h2 class="card__label" id="cap-h">Capacity</h2>
      {#if cap.over}<span class="card__pill card__pill--over"><Icon name="warning" size={12} /> Over</span>{/if}
    </div>

    {#if cap.plannedMin > 0}
      <div class="ringwrap">
        <Ring
          value={cap.fraction}
          over={cap.over}
          gradientId="cap-ring"
          label={`${cap.label}`}
        />
        <div class="ringwrap__center">
          <span class="ringwrap__big">{fmtDuration(cap.plannedMin)}</span>
          <span class="ringwrap__sub">of {fmtDuration(cap.budgetMin)}</span>
        </div>
      </div>
      <p class="card__foot" class:card__foot--over={cap.over}>
        {#if cap.over}Over budget by {fmtDuration(cap.plannedMin - cap.budgetMin)}{:else}{fmtDuration(Math.max(0, cap.budgetMin - cap.plannedMin))} of headroom left{/if}
        {#if cap.unestimated > 0}<br /><span class="muted small">+{cap.unestimated} with no estimate</span>{/if}
      </p>
    {:else}
      <div class="card__blank">
        <p class="muted">
          {#if cap.unestimated > 0}
            {cap.unestimated} task{cap.unestimated === 1 ? "" : "s"} due today {cap.unestimated === 1 ? "has" : "have"} no
            <code>est:</code>. Add one to plan your day.
          {:else}
            No timed work due today. Add <code>est:</code> to a task to plan capacity.
          {/if}
        </p>
      </div>
    {/if}
  </section>

  <!-- ── MOMENTUM: completed-today + open, both from real data ───────────── -->
  <section class="card card--stats" aria-labelledby="mo-h">
    <div class="card__head">
      <h2 class="card__label" id="mo-h">Momentum</h2>
    </div>
    <div class="stats">
      <div class="stat-tile">
        <span class="stat-tile__n">{completedToday}</span>
        <span class="stat-tile__k">done today</span>
      </div>
      <div class="stat-tile">
        <span class="stat-tile__n">{openCount}</span>
        <span class="stat-tile__k">open</span>
      </div>
      <div class="stat-tile">
        <span class="stat-tile__n">{noteCount}</span>
        <span class="stat-tile__k">notes</span>
      </div>
    </div>
  </section>

  <!-- ── WEEK SPARKLINE: 7-day completion trend (omitted if no real data) ─── -->
  {#if hasWeekData}
    <section class="card card--trend" aria-labelledby="wk-h">
      <div class="card__head">
        <h2 class="card__label" id="wk-h">This week</h2>
        <span class="card__count">{weekTotal} done</span>
      </div>
      <Sparkline data={weekSeries} label={`${weekTotal} tasks completed in the last 7 days`} />
      <p class="card__foot muted small">Tasks completed · last 7 days</p>
    </section>
  {/if}
  </div>
</div>

<style>
  /* Friendly load-failure state inside the Focus card (W7) — never expose the
     raw error object; offer a retry. */
  .loaderr {
    display: flex;
    align-items: center;
    gap: 12px;
    margin: 12px 0;
  }
  .loaderr__msg {
    margin: 0;
    color: var(--fg-soft);
  }

  /* ── hero ──────────────────────────────────────────────────────────────── */
  .hero {
    margin-bottom: var(--space-7);
  }
  .hero__eyebrow {
    margin: 0 0 var(--space-1);
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    letter-spacing: var(--tracking-caps);
    text-transform: uppercase;
    color: var(--accent-color);
  }
  .hero__title {
    /* editorial serif display title (bigger, more confident than the default h1) */
    font-size: var(--text-display-sm);
    letter-spacing: var(--tracking-display);
    margin: 0 0 var(--space-2);
  }
  .hero__date {
    margin: 0;
    font-family: var(--font-mono);
    font-size: var(--text-callout);
    letter-spacing: var(--tracking-caps);
    text-transform: uppercase;
    color: var(--muted);
  }

  /* ── bento grid ────────────────────────────────────────────────────────── */
  /* Two INDEPENDENT column stacks (not a shared cell grid): a wider main column
     and a narrower rail. Because each column is its own flex stack, the columns
     never share grid rows, so a short Focus card can't leave a void beside the
     rail (nor the feed strand off to the bottom-left). */
  .bento {
    display: grid;
    grid-template-columns: 2fr 1fr;
    gap: var(--space-5);
    align-items: start;
  }
  /* main: Focus → Recent activity.  rail: Capacity → Momentum → This week. */
  .bento__col {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
    min-width: 0;
  }
  /* On a clear day TaskRows draws its own centered "all clear" hero; the keycap
     hint below would just be noise under it, so hide it. */
  .card--clear .card__hint {
    display: none;
  }

  /* ── card shell (glass bento) ──────────────────────────────────────────── */
  .card {
    position: relative;
    padding: var(--space-6);
    border-radius: var(--radius-lg);
    background: color-mix(in srgb, var(--bg-elevated) 82%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    box-shadow: var(--shadow-bento);
    min-width: 0;
  }
  .card__head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-3);
    margin-bottom: var(--space-4);
  }
  .card__label {
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    color: var(--muted);
    margin: 0;
  }
  .card__count {
    font-family: var(--font-mono);
    font-size: var(--text-callout);
    color: var(--label-secondary);
    font-variant-numeric: tabular-nums;
  }
  .card__all {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font-size: var(--text-callout);
    color: var(--muted);
  }
  .card__all:hover {
    color: var(--accent-color);
    text-decoration: none;
  }
  .card__pill {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    padding: 1px 7px;
    border-radius: 999px;
  }
  .card__pill--over {
    color: var(--red);
    background: color-mix(in srgb, var(--red) 12%, transparent);
  }
  .card__blank {
    padding: var(--space-2) 0 var(--space-3);
  }
  .card__hint {
    margin: var(--space-4) 0 0;
    font-size: var(--text-callout);
    color: var(--muted);
  }
  .card__hint kbd {
    font-family: var(--font-mono);
    font-size: 0.92em;
    background: var(--bg-inset);
    border: 0.5px solid var(--separator-strong);
    border-radius: var(--radius-xs);
    padding: 0 5px;
    color: var(--label-secondary);
  }
  .card__foot {
    margin: var(--space-4) 0 0;
    font-size: var(--text-body);
    color: var(--label-secondary);
    text-align: center;
  }
  .card__foot--over {
    color: var(--red);
  }

  /* ── capacity ring ─────────────────────────────────────────────────────── */
  .ringwrap {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-2) 0;
  }
  .ringwrap__center {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 1px;
    pointer-events: none;
  }
  .ringwrap__big {
    font-family: var(--font-serif);
    font-size: var(--text-title1);
    font-weight: 600;
    letter-spacing: var(--tracking-title);
    color: var(--label-primary);
    font-variant-numeric: tabular-nums;
  }
  .ringwrap__sub {
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    color: var(--muted);
  }

  /* ── momentum stat tiles ───────────────────────────────────────────────── */
  .stats {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: var(--space-3);
  }
  .stat-tile {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2px;
    padding: var(--space-4) var(--space-2);
    border-radius: var(--radius-md);
    background: var(--bg-inset);
    box-shadow: var(--glass-hairline);
  }
  .stat-tile__n {
    font-family: var(--font-serif);
    font-size: var(--text-title1);
    font-weight: 600;
    letter-spacing: var(--tracking-title);
    color: var(--label-primary);
    font-variant-numeric: tabular-nums;
    line-height: 1.1;
  }
  .stat-tile__k {
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    color: var(--muted);
    text-align: center;
  }

  /* ── recent-activity feed (restyled: status dots + hairlines + mono when) ─ */
  .feed {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .feed__item {
    display: flex;
    align-items: baseline;
    gap: var(--space-3);
    padding: 7px 0;
    border-bottom: 0.5px solid var(--separator);
    min-width: 0;
  }
  .feed__item:last-child {
    border-bottom: 0;
  }
  /* Action glyph: a per-action icon (a non-colour cue) tinted by the same hue the
     old dot used, so the action reads for colour-blind users too (finding 11). */
  .feed__glyph {
    flex: none;
    align-self: center;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: var(--muted);
  }
  .feed__glyph--added,
  .feed__glyph--created {
    color: var(--green);
  }
  .feed__glyph--completed {
    color: var(--accent-color);
  }
  .feed__glyph--updated {
    color: var(--teal);
  }
  .feed__glyph--archived,
  .feed__glyph--deleted {
    color: var(--label-quaternary);
  }
  /* Spoken verb — present for assistive tech and as a calm inline label so the
     action never relies on colour alone. */
  .feed__verb {
    flex: none;
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    color: var(--muted);
  }
  .feed__title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--text-body);
    color: var(--label-secondary);
    transition: color var(--motion-fast) var(--ease);
  }
  a.feed__title:hover {
    color: var(--accent-color);
    text-decoration: none;
  }
  .feed__when {
    flex: none;
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }

  /* ── responsive: collapse to a single column with a sensible reading order ─ */
  /* Below 1040px the two columns dissolve (display:contents) so all five cards
     flow in one column; `order` interleaves them as Focus → Capacity → Momentum
     → This week → Recent activity. */
  @media (max-width: 1040px) {
    .bento {
      grid-template-columns: 1fr;
    }
    .bento__col {
      display: contents;
    }
    .card--focus {
      order: 1;
    }
    .card--capacity {
      order: 2;
    }
    .card--stats {
      order: 3;
    }
    .card--trend {
      order: 4;
    }
    .card--feed {
      order: 5;
    }
  }
</style>
