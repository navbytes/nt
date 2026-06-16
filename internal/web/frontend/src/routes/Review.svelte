<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import type { Task, ReviewResponse } from "../lib/api";
  import TaskRow from "../lib/TaskRow.svelte";
  import Icon from "../lib/Icon.svelte";

  // `embedded` renders Review as a tab inside Tasks (no own page title — the
  // Tasks header + the active "Review" tab already label it).
  let { embedded = false }: { embedded?: boolean } = $props();

  const reviewQ = createQuery({ queryKey: ["review"], queryFn: api.review });

  const total = (r: ReviewResponse): number =>
    (r.overdue?.length ?? 0) +
    (r.stale?.length ?? 0) +
    (r.undated?.length ?? 0) +
    (r.stuckProjects?.length ?? 0);
</script>

{#if !embedded}<div class="pagehead"><h1>Review</h1></div>{/if}
<p class="muted" class:rev-sub--embedded={embedded}>Weekly triage — overdue, stale, undated, and stuck projects that need a decision.</p>

{#snippet bucket(title: string, tasks: Task[], danger = false)}
  {#if tasks.length}
    <section class="rev">
      <h2 class="rev__head" class:rev__head--danger={danger}>
        {#if danger}<Icon name="flame" size={13} filled />{/if}
        <span class="rev__title">{title}</span>
        <span class="rev__count">{tasks.length}</span>
      </h2>
      <ul class="rows">
        {#each tasks as t (t.id)}
          <TaskRow {t} />
        {/each}
      </ul>
    </section>
  {/if}
{/snippet}

{#if $reviewQ.isPending}
  <p class="muted">Loading…</p>
{:else if $reviewQ.error}
  <p class="error">Couldn't load the review.</p>
{:else if $reviewQ.data}
  {#if total($reviewQ.data) === 0}
    <div class="empty empty--hero">
      <div class="empty__art" aria-hidden="true">
        <span class="empty__halo"></span>
        <Icon name="check" size={28} strokeWidth={2.2} />
      </div>
      <p class="empty__lead">You're on top of it</p>
      <p class="muted">Nothing overdue, stale, undated, or stuck right now. Inbox zero for your task triage.</p>
    </div>
  {/if}
  {@render bucket("Overdue", $reviewQ.data.overdue, true)}
  {@render bucket(`Stale — open ≥ ${$reviewQ.data.staleDays}d, no progress`, $reviewQ.data.stale)}
  {@render bucket("No due date — schedule or drop", $reviewQ.data.undated)}
  {#if $reviewQ.data.stuckProjects?.length}
    <section class="rev">
      <h2 class="rev__head">
        <span class="rev__title">Stuck projects — every open task is blocked</span>
        <span class="rev__count">{$reviewQ.data.stuckProjects.length}</span>
      </h2>
      <div class="rev__chips">
        {#each $reviewQ.data.stuckProjects as p (p)}
          <a class="chip chip--proj" href={`/search?tag=${encodeURIComponent(p)}`}>+{p}</a>
        {/each}
      </div>
    </section>
  {/if}
{/if}

<style>
  .rev {
    margin-top: 28px;
  }
  /* Bucket header: a mono microlabel (matching the group headers across Tasks)
     with a tabular count chip. Overdue runs hot — red label + flame — so the
     pile that needs a decision most reads first. Colour on the content surface
     (AA both themes). */
  .rev__head {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0 0 6px;
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    color: var(--label-secondary);
  }
  .rev__title {
    font-weight: 600;
  }
  .rev__head--danger {
    color: var(--red);
  }
  .rev__count {
    font-size: var(--text-footnote);
    color: var(--label-secondary);
    font-family: var(--font-mono);
    font-variant-numeric: tabular-nums;
    background: var(--bg-inset);
    border-radius: 999px;
    padding: 0 6px;
    line-height: 1.7;
  }
  .rev__head--danger .rev__count {
    color: var(--red);
    background: color-mix(in srgb, var(--red) 11%, transparent);
  }
  .rev__chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 10px;
  }
  /* Project chip with a subtle accent edge — matches the list rows' project chip. */
  .chip--proj {
    color: var(--accent-color);
    box-shadow: inset 0 0 0 0.5px color-mix(in srgb, var(--accent-color) 40%, transparent);
  }
  .chip--proj:hover {
    background: var(--accent-tint);
    text-decoration: none;
  }
  /* "You're on top of it" hero — a soft green halo bloom behind the check, the
     calm cousin of the onboarding spectral motif. No text sits on the glow. */
  .empty__art {
    position: relative;
    overflow: visible;
  }
  .empty__halo {
    position: absolute;
    width: 64px;
    height: 64px;
    border-radius: 50%;
    background: var(--green);
    opacity: 0.18;
    filter: blur(12px);
    z-index: 0;
  }
  .empty__art :global(.icon) {
    position: relative;
    z-index: 1;
  }
  /* As a Tasks tab there's no page title above; give the subtitle some air. */
  .rev-sub--embedded {
    margin-top: 12px;
  }
</style>
