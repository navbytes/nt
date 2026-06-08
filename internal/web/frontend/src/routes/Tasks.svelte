<script lang="ts">
  import TaskRows from "../lib/TaskRows.svelte";

  let { canEdit }: { canEdit: boolean } = $props();

  // Persist the chosen grouping so it sticks across visits.
  let view = $state<"agenda" | "status">(
    (localStorage.getItem("nt-tasks-view") as "agenda" | "status") ?? "agenda",
  );
  $effect(() => localStorage.setItem("nt-tasks-view", view));
</script>

<div class="pagehead">
  <h1>Tasks</h1>
  <div class="seg" role="group" aria-label="Group tasks by">
    <button class:seg--on={view === "agenda"} onclick={() => (view = "agenda")}>Agenda</button>
    <button class:seg--on={view === "status"} onclick={() => (view = "status")}>Status</button>
  </div>
</div>

<TaskRows {canEdit} showAdd={true} {view} />

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
