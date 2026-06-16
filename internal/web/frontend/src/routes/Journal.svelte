<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import { loc, navigate } from "../lib/router.svelte";
  import NoteView from "./NoteView.svelte";
  import Icon from "../lib/Icon.svelte";

  const qc = useQueryClient();
  const journalQ = createQuery({ queryKey: ["journal"], queryFn: api.journal });

  // The active day is the ?date= query (read from the router so it stays live
  // while embedded in the Notes "Daily" view), falling back to today.
  const active = $derived(loc.query.get("date") || $journalQ.data?.today || "");
  const handle = $derived($journalQ.data?.days.find((d) => d.date === active)?.handle ?? "");
  const isToday = $derived(!!$journalQ.data && active === $journalQ.data.today);

  // Parse "YYYY-MM-DD" into a local Date (a fixed tuple keeps TS strict happy).
  function asDate(iso: string): Date {
    const p = iso.split("-");
    return new Date(Number(p[0]), Number(p[1]) - 1, Number(p[2]));
  }
  function fmtISO(dt: Date): string {
    return `${dt.getFullYear()}-${String(dt.getMonth() + 1).padStart(2, "0")}-${String(dt.getDate()).padStart(2, "0")}`;
  }
  function addDays(iso: string, n: number): string {
    const dt = asDate(iso);
    dt.setDate(dt.getDate() + n);
    return fmtISO(dt);
  }

  function go(d: string) {
    navigate(`/journal?date=${d}`);
  }

  // Render a date as "Mon, Jun 8 2026".
  function pretty(iso: string): string {
    if (!iso) return "";
    return asDate(iso).toLocaleDateString(undefined, {
      weekday: "short",
      month: "short",
      day: "numeric",
      year: "numeric",
    });
  }

  let creating = $state(false);
  async function startEntry() {
    if (!active || creating) return;
    creating = true;
    try {
      await api.noteCreate(active, $journalQ.data?.folder ?? "journal");
      await qc.invalidateQueries({ queryKey: ["journal"] });
      await qc.invalidateQueries({ queryKey: ["notes"] });
    } finally {
      creating = false;
    }
  }
</script>

<div class="jrnl">
  <header class="jrnl__bar">
    <div class="jrnl__head">
      <p class="jrnl__eyebrow">Daily</p>
      <div class="jrnl__nav">
        <button class="jrnl__step" aria-label="Previous day" onclick={() => go(addDays(active, -1))}
          ><Icon name="chevron-left" size={16} /></button
        >
        <h1 class="jrnl__date">
          {pretty(active)}
          {#if isToday}<span class="jrnl__today">today</span>{/if}
        </h1>
        <button class="jrnl__step" aria-label="Next day" onclick={() => go(addDays(active, 1))}
          ><Icon name="chevron-right" size={16} /></button
        >
      </div>
    </div>
    {#if !isToday && $journalQ.data}
      <button class="jrnl__jump" onclick={() => go($journalQ.data.today)}>
        <Icon name="sun" size={14} /> Jump to today
      </button>
    {/if}
  </header>

  {#if $journalQ.isPending}
    <p class="muted">Loading…</p>
  {:else if handle}
    {#key handle}
      <NoteView {handle} />
    {/key}
  {:else}
    <div class="jrnl__empty empty empty--hero">
      <span class="empty__art empty__art--onboard"><Icon name="calendar" size={28} /></span>
      <p class="empty__lead">Nothing written for {pretty(active)} yet.</p>
      <p class="muted">Capture the day — what happened, what you decided, what's on your mind.</p>
      <button class="btn jrnl__start" disabled={creating} onclick={startEntry}>
        <Icon name="plus" size={14} /> {creating ? "Creating…" : "Start this entry"}
      </button>
    </div>
  {/if}

  {#if $journalQ.data && $journalQ.data.days.length > 0}
    <aside class="jrnl__recent">
      <div class="jrnl__recent-head">Recent entries</div>
      <ul>
        {#each $journalQ.data.days.slice(0, 14) as d (d.date)}
          <li>
            <a class:active={d.date === active} href={`/journal?date=${d.date}`}>{pretty(d.date)}</a>
          </li>
        {/each}
      </ul>
    </aside>
  {/if}
</div>

<style>
  .jrnl__bar {
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    gap: var(--space-5);
    margin-bottom: var(--space-6);
  }
  .jrnl__head {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }
  /* Mono eyebrow above the serif date — matches the Notes/Home hero language. */
  .jrnl__eyebrow {
    margin: 0;
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    letter-spacing: var(--tracking-caps);
    text-transform: uppercase;
    color: var(--accent-color);
  }
  .jrnl__nav {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }
  /* Serif display date — confident, editorial. */
  .jrnl__date {
    margin: 0;
    font-family: var(--font-serif);
    font-size: var(--text-display-sm);
    letter-spacing: var(--tracking-display);
    font-weight: 600;
  }
  /* "Today" badge — a spectral-tinted pill (the date carries no gradient under
     its text; the badge sits beside it). */
  .jrnl__today {
    font-family: var(--font-mono);
    font-size: 0.42em;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    color: var(--spectral-2);
    background: var(--accent-tint);
    box-shadow: inset 0 0 0 0.5px color-mix(in srgb, var(--spectral-2) 45%, transparent);
    border-radius: 999px;
    padding: 3px 9px;
    vertical-align: middle;
    margin-left: var(--space-3);
  }
  /* Glass step buttons (prev / next day). */
  .jrnl__step {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 32px;
    min-height: 30px;
    background: color-mix(in srgb, var(--bg-elevated) 70%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    border: 0.5px solid var(--separator);
    box-shadow: var(--glass-hairline);
    border-radius: var(--radius-sm);
    color: var(--label-secondary);
    cursor: pointer;
    line-height: 1;
    padding: 4px 10px;
    transition:
      background var(--motion-fast) var(--ease),
      color var(--motion-fast) var(--ease),
      border-color var(--motion-fast) var(--ease),
      transform var(--motion-fast) var(--ease);
  }
  .jrnl__step:hover {
    color: var(--fg);
    border-color: var(--separator-strong);
  }
  .jrnl__step:active {
    transform: scale(0.94);
  }
  /* "Jump to today" — a glassy ghost pill with a sun glyph. */
  .jrnl__jump {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    flex: none;
    background: color-mix(in srgb, var(--bg-elevated) 70%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    border: 0.5px solid var(--separator);
    box-shadow: var(--glass-hairline);
    border-radius: var(--radius-sm);
    color: var(--label-secondary);
    cursor: pointer;
    font-size: var(--text-body);
    padding: 6px 12px;
    transition:
      color var(--motion-fast) var(--ease),
      border-color var(--motion-fast) var(--ease);
  }
  .jrnl__jump:hover {
    color: var(--accent-color);
    border-color: color-mix(in srgb, var(--accent-color) 40%, transparent);
  }
  .jrnl__jump :global(.icon) {
    color: var(--pri-b);
  }
  /* Empty state uses the shared .empty--hero; only spacing tuned here. */
  .jrnl__empty {
    margin: var(--space-9) auto;
  }
  .jrnl__start {
    margin-top: var(--space-5);
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  /* Recent entries — glassy date chips; the active day glows spectral. */
  .jrnl__recent {
    margin-top: var(--space-9);
    border-top: 0.5px solid var(--separator);
    padding-top: var(--space-5);
  }
  .jrnl__recent-head {
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    color: var(--muted);
    margin-bottom: var(--space-3);
  }
  .jrnl__recent ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }
  .jrnl__recent a {
    display: inline-block;
    font-family: var(--font-mono);
    font-size: var(--text-callout);
    letter-spacing: var(--tracking-body);
    padding: 4px 11px;
    border-radius: 999px;
    background: var(--fill);
    border: 0.5px solid var(--separator);
    color: var(--label-secondary);
    transition:
      background var(--motion-fast) var(--ease),
      color var(--motion-fast) var(--ease),
      border-color var(--motion-fast) var(--ease);
  }
  .jrnl__recent a:hover {
    color: var(--label-primary);
    border-color: var(--separator-strong);
    text-decoration: none;
  }
  .jrnl__recent a.active {
    background: var(--accent-tint);
    color: var(--spectral-2);
    border-color: color-mix(in srgb, var(--spectral-2) 45%, transparent);
    font-weight: 500;
  }

  @media (max-width: 640px) {
    .jrnl__bar {
      flex-wrap: wrap;
    }
    .jrnl__date {
      font-size: var(--text-title1);
    }
  }
</style>
