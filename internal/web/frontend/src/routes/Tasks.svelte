<script lang="ts">
  import TaskRows from "../lib/TaskRows.svelte";
  import Board from "../lib/Board.svelte";
  import Review from "./Review.svelte";
  import Icon from "../lib/Icon.svelte";

  type TaskView = "agenda" | "status" | "board" | "review";
  // initialView is set when the app lands on /review (the old Review route now
  // resolves to the Tasks "Review" tab). viewName (/tasks?view=…) recalls a
  // saved smart view instead — the server applies it, we just render the rows.
  let {
    initialView,
    viewName = "",
  }: { initialView?: TaskView; viewName?: string } = $props();

  // Persist the chosen view so it sticks across visits; an explicit initialView
  // (from /review) wins on landing.
  let view = $state<TaskView>(
    initialView ?? (localStorage.getItem("nt-tasks-view") as TaskView) ?? "agenda",
  );
  $effect(() => localStorage.setItem("nt-tasks-view", view));

  // The Board (Kanban columns + drag) is a pointer affordance; on a phone the
  // Status list is the better answer, so Board falls back to it under the
  // mobile breakpoint.
  let narrow = $state(false);
  $effect(() => {
    const mq = window.matchMedia("(max-width: 640px)");
    const sync = () => (narrow = mq.matches);
    sync();
    mq.addEventListener("change", sync);
    return () => mq.removeEventListener("change", sync);
  });

  // Quick filter (W12): shown for the list layouts (agenda/status + saved views,
  // and Board only when it falls back to the list on mobile). Board's own
  // columns and Review's triage panes are excluded — they aren't a flat list.
  let filter = $state("");
  const showFilter = $derived(
    !!viewName || (view !== "review" && !(view === "board" && !narrow)),
  );
  // Reset the filter when switching to a layout that doesn't show it.
  $effect(() => {
    if (!showFilter) filter = "";
  });
</script>

<div class="pagehead">
  <h1>Tasks</h1>
  {#if viewName}
    <!-- A recalled saved view: the pill names it; × returns to the regular tabs. -->
    <span class="viewpill">
      {viewName}
      <a class="viewpill__close" href="/tasks" title="Leave this view" aria-label="Leave this view"><Icon name="close" size={13} /></a>
    </span>
  {:else}
    <div class="seg" role="group" aria-label="View tasks as">
      <button class:seg--on={view === "agenda"} aria-pressed={view === "agenda"} onclick={() => (view = "agenda")}>Agenda</button>
      <button class:seg--on={view === "status"} aria-pressed={view === "status"} onclick={() => (view = "status")}>Status</button>
      <button class:seg--on={view === "board"} aria-pressed={view === "board"} onclick={() => (view = "board")}>Board</button>
      <button class:seg--on={view === "review"} aria-pressed={view === "review"} onclick={() => (view = "review")}>Review</button>
    </div>
  {/if}
</div>

{#if showFilter}
  <div class="qfilter">
    <input
      bind:value={filter}
      onkeydown={(e) => e.key === "Escape" && (filter = "")}
      placeholder="Filter…  @tag +project !pri words"
      aria-label="Filter tasks"
      autocomplete="off"
    />
    {#if filter}<button class="qfilter__clear" onclick={() => (filter = "")} aria-label="Clear filter"><Icon name="close" size={14} /></button>{/if}
  </div>
{/if}

{#if viewName}
  <TaskRows {viewName} {filter} />
{:else if view === "board" && !narrow}
  <Board />
{:else if view === "review"}
  <Review embedded />
{:else}
  <TaskRows showAdd={true} view={view === "board" ? "status" : view} {filter} />
{/if}

<style>
  .qfilter {
    position: relative;
    margin: 4px 0 12px;
  }
  .qfilter input {
    width: 100%;
    padding: 6px 28px 6px 12px;
    font-size: 0.9rem;
    background: var(--fill);
    border: 0.5px solid var(--separator);
    border-radius: var(--radius-sm);
    color: var(--fg);
  }
  .qfilter input:focus {
    border-color: var(--accent);
  }
  .qfilter__clear {
    position: absolute;
    right: 6px;
    top: 50%;
    transform: translateY(-50%);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: none;
    border: none;
    color: var(--muted);
    cursor: pointer;
    line-height: 1;
    padding: 2px 6px;
    border-radius: var(--radius-xs);
  }
  .qfilter__clear:hover {
    color: var(--fg);
  }
  .pagehead {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
  }
  /* macOS AppKit segmented control: a gray track holding an elevated pill on
     the selected segment (not a saturated accent fill). */
  .seg {
    display: flex;
    gap: 2px;
    padding: 2px;
    background: var(--fill);
    border: 0.5px solid var(--separator);
    border-radius: var(--radius-sm);
  }
  .seg button {
    padding: 4px 12px;
    background: transparent;
    border: none;
    border-radius: calc(var(--radius-sm) - 1px);
    color: var(--fg-soft);
    cursor: pointer;
    font-size: 0.85rem;
    transition:
      background var(--motion-fast) var(--ease),
      color var(--motion-fast) var(--ease),
      box-shadow var(--motion-fast) var(--ease);
  }
  .seg--on {
    background: var(--bg-elevated);
    color: var(--fg);
    box-shadow: var(--shadow-control);
  }
  .viewpill {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 3px 6px 3px 12px;
    border: 1px solid var(--accent);
    border-radius: 999px;
    color: var(--accent);
    font-size: 0.85rem;
    font-weight: 600;
  }
  .viewpill__close {
    display: inline-flex;
    align-items: center;
    color: var(--muted);
    font-weight: 400;
    padding: 2px 4px;
    border-radius: 999px;
  }
  .viewpill__close:hover {
    color: var(--fg);
    background: var(--fill);
  }
</style>
