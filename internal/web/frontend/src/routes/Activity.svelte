<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api, type ActivityDay } from "../lib/api";
  import Icon from "../lib/Icon.svelte";
  import { fmtTime } from "../lib/text";

  let source = $state("");
  const activityQ = createQuery({ queryKey: ["activity", ""], queryFn: () => api.activity() });

  const sources = $derived($activityQ.data?.sources ?? []);
  const days = $derived(filterDays($activityQ.data?.days ?? [], source));
  const eventCount = $derived(days.reduce((n, d) => n + (d.events?.length ?? 0), 0));

  function filterDays(all: ActivityDay[], src: string): ActivityDay[] {
    if (!src) return all;
    return all
      .map((d) => ({ date: d.date, events: (d.events ?? []).filter((e) => e.source === src) }))
      .filter((d) => d.events.length > 0);
  }

  // A friendly day header: "Today" / "Yesterday" / weekday + date for the recent
  // past, falling back to the raw ISO for anything unparseable. Mirrors Home's
  // relDay conventions (date-only precision; we never invent a clock time).
  function dayLabel(iso: string): string {
    const [y, m, d] = iso.split("-").map(Number);
    if (!y || !m || !d) return iso;
    const day = new Date(y, m - 1, d);
    const today = new Date();
    const t0 = new Date(today.getFullYear(), today.getMonth(), today.getDate());
    const diff = Math.round((day.getTime() - t0.getTime()) / 86400000);
    if (diff === 0) return "Today";
    if (diff === -1) return "Yesterday";
    const wd = day.toLocaleDateString(undefined, { weekday: "long" });
    const md = day.toLocaleDateString(undefined, { month: "short", day: "numeric" });
    return diff >= -6 ? `${wd} · ${md}` : md;
  }

  // A mono timestamp from the RFC3339 `when`, when it actually carries a wall
  // time. Many events (completions) are date-only at midnight — for those we show
  // a neutral dot rather than a fake "12am".
  function evTime(when: string): string {
    if (!when.includes("T")) return "";
    const hhmm = when.slice(11, 16);
    if (hhmm === "00:00") return "";
    return fmtTime(hhmm);
  }

  // Past-tense verb for the action badge ("added" / "completed" / "updated" /
  // "archived" …). Falls back to the raw action for anything new from the server.
  const VERB: Record<string, string> = {
    added: "added",
    created: "created",
    completed: "done",
    updated: "updated",
    archived: "archived",
    deleted: "deleted",
  };
  function verb(action: string): string {
    return VERB[action] ?? action;
  }
</script>

<header class="ahead">
  <div class="ahead__lead">
    <p class="ahead__eyebrow">Provenance</p>
    <h1 class="ahead__title">Activity</h1>
    <p class="ahead__meta">Every add, completion &amp; edit — yours and your agents'</p>
  </div>
  <select bind:value={source} class="ahead__filter" aria-label="Filter by source">
    <option value="">all sources</option>
    {#each sources as s (s)}<option value={s}>{s}</option>{/each}
  </select>
</header>

{#if $activityQ.isPending}
  <p class="muted">Loading…</p>
{:else if $activityQ.error}
  <div class="empty empty--hero">
    <span class="empty__art empty__art--err"><Icon name="warning" size={24} /></span>
    <p class="empty__lead">Couldn't load activity</p>
    <button class="btn btn--ghost btn--sm" onclick={() => $activityQ.refetch()}>Try again</button>
  </div>
{:else if eventCount === 0}
  <div class="empty empty--hero">
    <span class="empty__art empty__art--onboard"><Icon name="activity" size={26} /></span>
    <p class="empty__lead">{source ? `No activity from “${source}”` : "No activity yet"}</p>
    <p class="muted">Your adds, completions, and edits show up here as you (or your agents) work.</p>
  </div>
{:else}
  <div class="timeline">
    {#each days as day (day.date)}
      <section class="tl-day">
        <h2 class="tl-day__head">{dayLabel(day.date)}</h2>
        <ul class="tl-rows">
          {#each day.events as ev (ev.when + ev.title)}
            {@const time = evTime(ev.when)}
            <li class="tl-row">
              <span class="tl-row__rail" aria-hidden="true">
                <span class="tl-dot tl-dot--{ev.action}"></span>
              </span>
              <span class="tl-row__time">{time || "—"}</span>
              <span class="tl-badge tl-badge--{ev.action}">{verb(ev.action)}</span>
              <span class="tl-row__kind">{ev.kind}</span>
              {#if ev.url}
                <a class="tl-row__title" href={ev.url} title={ev.title}>{ev.title}</a>
              {:else}
                <span class="tl-row__title" title={ev.title}>{ev.title}</span>
              {/if}
              {#if ev.source}<span class="tl-row__src">{ev.source}</span>{/if}
            </li>
          {/each}
        </ul>
      </section>
    {/each}
  </div>
{/if}

<style>
  /* ── editorial header + source filter ────────────────────────────────────── */
  .ahead {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-5);
    margin-bottom: var(--space-6);
  }
  .ahead__eyebrow {
    margin: 0 0 var(--space-1);
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    letter-spacing: var(--tracking-caps);
    text-transform: uppercase;
    color: var(--accent-color);
  }
  .ahead__title {
    font-size: var(--text-display-sm);
    letter-spacing: var(--tracking-display);
    margin: 0 0 var(--space-2);
  }
  .ahead__meta {
    margin: 0;
    font-family: var(--font-mono);
    font-size: var(--text-callout);
    letter-spacing: var(--tracking-caps);
    text-transform: uppercase;
    color: var(--muted);
  }
  .ahead__filter {
    flex: none;
    font: inherit;
    font-size: var(--text-body);
    padding: 5px 10px;
    background: var(--fill);
    border: 0.5px solid var(--separator-strong);
    border-radius: var(--radius-sm);
    color: var(--fg);
  }

  /* ── timeline ────────────────────────────────────────────────────────────── */
  .tl-day {
    margin-bottom: var(--space-6);
  }
  .tl-day__head {
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    color: var(--muted);
    margin: 0 0 var(--space-3);
    padding-left: 26px; /* align over the rail */
  }
  .tl-rows {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .tl-row {
    display: flex;
    align-items: baseline;
    gap: var(--space-3);
    padding: 7px 0;
    min-width: 0;
    border-bottom: 0.5px solid var(--separator);
  }
  .tl-row:last-child {
    border-bottom: 0;
  }
  /* The spine: a vertical hairline with a status dot centred on it. The hairline
     is drawn as a pseudo-element so consecutive rows read as one continuous
     thread down the day. */
  .tl-row__rail {
    position: relative;
    flex: none;
    align-self: stretch;
    width: 14px;
  }
  .tl-row__rail::before {
    content: "";
    position: absolute;
    left: 50%;
    top: 0;
    bottom: 0;
    width: 0.5px;
    transform: translateX(-50%);
    background: var(--separator);
  }
  .tl-dot {
    position: absolute;
    left: 50%;
    top: 0.55em;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    transform: translate(-50%, -50%);
    background: var(--muted);
    box-shadow: 0 0 0 3px var(--bg-content); /* punch the dot out of the rail line */
  }
  /* Status-dot colours — identical mapping to the dashboard's recent-activity
     feed so the two reads stay consistent. */
  .tl-dot--added,
  .tl-dot--created {
    background: var(--green);
  }
  .tl-dot--completed {
    background: var(--accent-color);
  }
  .tl-dot--updated {
    background: var(--teal);
  }
  .tl-dot--archived,
  .tl-dot--deleted {
    background: var(--label-quaternary);
  }
  .tl-row__time {
    flex: none;
    width: 56px;
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    color: var(--muted);
    font-variant-numeric: tabular-nums;
    text-align: right;
  }
  /* Action badge — mono microlabel, tinted by action (added=green,
     completed=accent, updated=teal). Colour + label carry the action; never
     relies on hue alone since the verb is spelled out. */
  .tl-badge {
    flex: none;
    font-family: var(--font-mono);
    font-size: var(--text-footnote);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    padding: 2px 7px;
    border-radius: 999px;
    background: var(--fill);
    color: var(--muted);
  }
  .tl-badge--added,
  .tl-badge--created {
    color: var(--green);
    background: color-mix(in srgb, var(--green) 13%, transparent);
  }
  .tl-badge--completed {
    color: var(--accent-color);
    background: var(--accent-tint);
  }
  .tl-badge--updated {
    color: var(--teal);
    background: color-mix(in srgb, var(--teal) 14%, transparent);
  }
  .tl-row__kind {
    flex: none;
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    color: var(--muted);
  }
  .tl-row__title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--text-body);
    color: var(--label-secondary);
    transition: color var(--motion-fast) var(--ease);
  }
  a.tl-row__title:hover {
    color: var(--accent-color);
    text-decoration: none;
  }
  .tl-row__src {
    flex: none;
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    color: var(--muted);
  }
  .empty__art--err {
    color: var(--red);
    background: color-mix(in srgb, var(--red) 13%, transparent);
  }

  @media (max-width: 640px) {
    .ahead {
      flex-direction: column;
      gap: var(--space-3);
    }
    .tl-row__kind {
      display: none;
    }
  }
</style>
