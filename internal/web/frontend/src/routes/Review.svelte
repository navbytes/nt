<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import type { Task, ReviewResponse } from "../lib/api";
  import TaskRow from "../lib/TaskRow.svelte";

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
        {title} <span class="rev__count">{tasks.length}</span>
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
    <p class="muted big">Nothing needs attention — you're on top of it ✨</p>
  {/if}
  {@render bucket("Overdue", $reviewQ.data.overdue, true)}
  {@render bucket(`Stale — open ≥ ${$reviewQ.data.staleDays}d, no progress`, $reviewQ.data.stale)}
  {@render bucket("No due date — schedule or drop", $reviewQ.data.undated)}
  {#if $reviewQ.data.stuckProjects?.length}
    <section class="rev">
      <h2 class="rev__head">
        Stuck projects — every open task is blocked
        <span class="rev__count">{$reviewQ.data.stuckProjects.length}</span>
      </h2>
      <div class="rev__chips">
        {#each $reviewQ.data.stuckProjects as p (p)}
          <a class="chip" href={`/search?tag=${encodeURIComponent(p)}`}>+{p}</a>
        {/each}
      </div>
    </section>
  {/if}
{/if}

<style>
  .rev {
    margin-top: 24px;
  }
  .rev__head {
    font-size: 0.95rem;
    font-weight: 600;
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0 0 4px;
  }
  .rev__head--danger {
    color: var(--red);
  }
  .rev__count {
    font-size: 0.74rem;
    color: var(--muted);
    font-family: var(--font-mono);
  }
  .rev__chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 8px;
  }
  .big {
    font-size: 1rem;
    margin-top: 20px;
  }
  /* As a Tasks tab there's no page title above; give the subtitle some air. */
  .rev-sub--embedded {
    margin-top: 12px;
  }
</style>
