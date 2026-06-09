<script lang="ts">
  import TaskRows from "../lib/TaskRows.svelte";
  import Board from "../lib/Board.svelte";
  import Review from "./Review.svelte";

  type TaskView = "agenda" | "status" | "board" | "review";
  // initialView is set when the app lands on /review (the old Review route now
  // resolves to the Tasks "Review" tab).
  let { canEdit, initialView }: { canEdit: boolean; initialView?: TaskView } = $props();

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
</script>

<div class="pagehead">
  <h1>Tasks</h1>
  <div class="seg" role="group" aria-label="View tasks as">
    <button class:seg--on={view === "agenda"} onclick={() => (view = "agenda")}>Agenda</button>
    <button class:seg--on={view === "status"} onclick={() => (view = "status")}>Status</button>
    <button class:seg--on={view === "board"} onclick={() => (view = "board")}>Board</button>
    <button class:seg--on={view === "review"} onclick={() => (view = "review")}>Review</button>
  </div>
</div>

{#if view === "board" && !narrow}
  <Board {canEdit} />
{:else if view === "review"}
  <Review {canEdit} embedded />
{:else}
  <TaskRows {canEdit} showAdd={true} view={view === "board" ? "status" : view} />
{/if}

<style>
  .pagehead {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
  }
  .seg {
    display: flex;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }
  .seg button {
    padding: 5px 12px;
    background: var(--bg-inset);
    border: none;
    color: var(--fg-soft);
    cursor: pointer;
    font-size: 0.85rem;
  }
  .seg--on {
    background: var(--accent) !important;
    color: #fff !important;
  }
</style>
