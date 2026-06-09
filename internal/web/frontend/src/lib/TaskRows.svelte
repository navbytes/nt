<script lang="ts">
  import { createQuery, createMutation, useQueryClient } from "@tanstack/svelte-query";
  import { api, type TaskGroup, type Task } from "./api";

  let {
    canEdit = false,
    statuses = null,
    showAdd = false,
    view = "status",
  }: {
    canEdit?: boolean;
    statuses?: string[] | null;
    showAdd?: boolean;
    /** "status" groups by doing/open/blocked/done; "agenda" groups by due date. */
    view?: "status" | "agenda";
  } = $props();

  const qc = useQueryClient();
  const tasksQ = createQuery({ queryKey: ["tasks"], queryFn: api.tasks });

  const set = (d: { groups: TaskGroup[] }) => qc.setQueryData(["tasks"], d);
  const doneMut = createMutation({ mutationFn: api.taskDone, onSuccess: set });
  const reopenMut = createMutation({ mutationFn: api.taskReopen, onSuccess: set });
  const addMut = createMutation({ mutationFn: api.taskNew, onSuccess: set });
  const statusMut = createMutation({
    mutationFn: (a: { id: string; status: string }) => api.taskStatus(a.id, a.status),
    onSuccess: set,
  });
  const deleteMut = createMutation({ mutationFn: api.taskDelete, onSuccess: set });

  let newText = $state("");

  const allGroups = $derived(($tasksQ.data?.groups ?? []) as TaskGroup[]);

  // Status view: the raw groups, optionally filtered to a status set.
  const statusGroups = $derived(allGroups.filter((g) => !statuses || statuses.includes(g.status)));

  function todayISO(): string {
    const d = new Date();
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
  }
  function plusDaysISO(n: number): string {
    const d = new Date();
    d.setDate(d.getDate() + n);
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
  }
  // A due value may carry a time-of-day ("2026-06-08T17:00"). dateOf gives the
  // date part for bucketing; fmtDue renders it readably for display.
  const dateOf = (due?: string) => (due ? due.slice(0, 10) : "");
  const fmtDue = (due: string) => (due.includes("T") ? due.replace("T", " ") : due);

  // Agenda view: re-bucket every task by its due date (the planner layout).
  const agendaGroups = $derived.by((): TaskGroup[] => {
    const today = todayISO();
    const weekEnd = plusDaysISO(7);
    const buckets = {
      Overdue: [] as Task[],
      Today: [] as Task[],
      "This week": [] as Task[],
      Later: [] as Task[],
      "No date": [] as Task[],
      Done: [] as Task[],
    };
    for (const g of allGroups) {
      for (const t of g.tasks) {
        const due = dateOf(t.due); // YYYY-MM-DD, ignoring any time-of-day suffix
        if (t.status === "done") buckets.Done.push(t);
        else if (!due) buckets["No date"].push(t);
        else if (due < today) buckets.Overdue.push(t);
        else if (due === today) buckets.Today.push(t);
        else if (due <= weekEnd) buckets["This week"].push(t);
        else buckets.Later.push(t);
      }
    }
    return (Object.entries(buckets) as [string, Task[]][])
      .filter(([, tasks]) => tasks.length > 0)
      .map(([status, tasks]) => ({ status, tasks }));
  });

  const groups = $derived(view === "agenda" ? agendaGroups : statusGroups);

  function add(e: SubmitEvent) {
    e.preventDefault();
    const t = newText.trim();
    if (t) {
      $addMut.mutate(t);
      newText = "";
    }
  }

  function toggleDoing(t: Task) {
    $statusMut.mutate({ id: t.id, status: t.status === "doing" ? "open" : "doing" });
  }
  function del(t: Task) {
    if (confirm(`Delete task "${t.text}"? (undoable with nt undo)`)) $deleteMut.mutate(t.id);
  }
</script>

{#if showAdd && canEdit}
  <form class="taskadd" onsubmit={add}>
    <input
      placeholder="Add a task…  (try: pay rent due:fri !high @home)"
      bind:value={newText}
      autocomplete="off"
    />
    <button class="btn" type="submit">Add</button>
  </form>
{/if}

{#if $tasksQ.isPending}
  <p class="muted">Loading tasks…</p>
{:else if $tasksQ.error}
  <p class="error">{String($tasksQ.error)}</p>
{:else}
  {#each groups as group (group.status)}
    <section class="group">
      <h2 class="group__title">{group.status} · {group.tasks.length}</h2>
      <ul class="rows">
        {#each group.tasks as t (t.id)}
          <li class="row" class:row--doing={t.status === "doing"}>
            {#if canEdit}
              {#if t.status === "done"}
                <button class="check check--done" title="Reopen" aria-label="Reopen task" onclick={() => $reopenMut.mutate(t.id)}>●</button>
              {:else}
                <button class="check" title="Mark done" aria-label="Mark task done" onclick={() => $doneMut.mutate(t.id)}>○</button>
              {/if}
            {/if}
            <span class="row__text" class:done={t.status === "done"} title={t.text}>{t.text}</span>
            {#if t.recur}<span class="row__recur" title="Recurring task">↻</span>{/if}
            {#if t.status === "doing"}<span class="status-pill status-pill--doing">doing</span>{/if}
            {#if t.status === "blocked"}<span class="status-pill status-pill--blocked" title={t.blocker ? `blocked by: ${t.blocker}` : "blocked"}>⊘ blocked</span>{/if}
            {#if t.project}<a class="chip" href={`/search?tag=${encodeURIComponent(t.project)}`}>+{t.project}</a>{/if}
            {#each t.tags ?? [] as tag (tag)}<a class="chip chip--tag" href={`/search?tag=${encodeURIComponent(tag)}`}>@{tag}</a>{/each}
            {#if t.due}<span class="row__due" class:row__due--over={dateOf(t.due) < todayISO() && t.status !== "done"}>{fmtDue(t.due)}</span>{/if}
            {#if t.source}<span class="src">{t.source}</span>{/if}
            {#if canEdit && t.status !== "done"}
              <span class="row__actions">
                <button class="rowbtn" title={t.status === "doing" ? "Stop (set open)" : "Start (set doing)"} onclick={() => toggleDoing(t)}>◐</button>
                <button class="rowbtn rowbtn--danger" title="Delete (undoable)" onclick={() => del(t)}>×</button>
              </span>
            {/if}
          </li>
        {/each}
      </ul>
      {#if group.tasks.length === 0}<p class="muted small">none</p>{/if}
    </section>
  {:else}
    <div class="empty">
      <p class="empty__lead">No tasks yet.</p>
      <p class="muted">
        Capture one above, or run <code>nt mcp install</code> so your AI agent can add and
        complete tasks through <code>nt_add</code> / <code>nt_done</code> — they share this same list.
      </p>
    </div>
  {/each}
{/if}

<style>
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .row--doing {
    border-left: 2px solid var(--accent);
    padding-left: 6px;
    margin-left: -8px;
  }
  .row__actions {
    margin-left: auto;
    display: flex;
    gap: 2px;
    opacity: 0;
    transition: opacity 0.1s;
  }
  .row:hover .row__actions {
    opacity: 1;
  }
  .rowbtn {
    background: none;
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    color: var(--muted);
    cursor: pointer;
    padding: 1px 6px;
    font-size: 0.9rem;
  }
  .rowbtn:hover {
    border-color: var(--border);
    color: var(--fg);
  }
  .rowbtn--danger:hover {
    color: var(--red);
    border-color: var(--red);
  }
  .status-pill {
    font-size: 0.68rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    padding: 1px 6px;
    border-radius: 999px;
  }
  .status-pill--doing {
    color: var(--accent);
    border: 1px solid var(--accent);
  }
  .status-pill--blocked {
    color: var(--red);
    border: 1px solid var(--red);
  }
  .chip--tag {
    color: var(--accent-2);
  }
  .row__due--over {
    color: var(--red);
    font-weight: 600;
  }
</style>
