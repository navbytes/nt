<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import { loc, navigate } from "../lib/router.svelte";
  import NoteView from "./NoteView.svelte";

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
    <div class="jrnl__nav">
      <button class="jrnl__step" aria-label="Previous day" onclick={() => go(addDays(active, -1))}>‹</button>
      <h1 class="jrnl__date">
        {pretty(active)}
        {#if isToday}<span class="jrnl__today">today</span>{/if}
      </h1>
      <button class="jrnl__step" aria-label="Next day" onclick={() => go(addDays(active, 1))}>›</button>
    </div>
    {#if !isToday && $journalQ.data}
      <button class="jrnl__jump" onclick={() => go($journalQ.data.today)}>Jump to today</button>
    {/if}
  </header>

  {#if $journalQ.isPending}
    <p class="muted">Loading…</p>
  {:else if handle}
    {#key handle}
      <NoteView {handle} />
    {/key}
  {:else}
    <div class="jrnl__empty">
      <p class="muted">No journal entry for {pretty(active)}.</p>
      <button class="btn" disabled={creating} onclick={startEntry}>
        {creating ? "Creating…" : "Start this entry"}
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
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    margin-bottom: 18px;
  }
  .jrnl__nav {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .jrnl__date {
    margin: 0;
    font-size: 1.4rem;
  }
  .jrnl__today {
    font-size: 0.6em;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--green);
    border: 1px solid var(--green);
    border-radius: 4px;
    padding: 1px 6px;
    vertical-align: middle;
    margin-left: 8px;
  }
  .jrnl__step {
    background: var(--bg-inset);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-soft);
    cursor: pointer;
    font-size: 1.1rem;
    line-height: 1;
    padding: 4px 12px;
  }
  .jrnl__step:hover {
    color: var(--accent);
    border-color: var(--accent);
  }
  .jrnl__jump {
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-soft);
    cursor: pointer;
    font-size: 0.85rem;
    padding: 5px 12px;
  }
  .jrnl__empty {
    padding: 32px 0;
  }
  .jrnl__recent {
    margin-top: 36px;
    border-top: 1px solid var(--border-soft);
    padding-top: 16px;
  }
  .jrnl__recent-head {
    font-size: 12px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--muted);
    margin-bottom: 8px;
  }
  .jrnl__recent ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .jrnl__recent a {
    display: inline-block;
    font-size: 0.8rem;
    padding: 3px 10px;
    border-radius: var(--radius-sm);
    background: var(--bg-inset);
    color: var(--fg-soft);
  }
  .jrnl__recent a.active {
    color: var(--accent);
    font-weight: 600;
  }
</style>
