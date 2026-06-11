<script lang="ts">
  import { createQuery, createMutation, useQueryClient } from "@tanstack/svelte-query";
  import { api, type TaskGroup, type Task } from "./api";
  import TaskRow from "./TaskRow.svelte";
  import { priorityRank } from "./text";

  // Within a bucket, float the most important work up: priority first (A→Z, then
  // unprioritised), then the earliest due date. Done stays in store order (most
  // recently completed already trails). Stable + non-mutating (operates on a copy).
  function byUrgency(a: Task, b: Task): number {
    const pr = priorityRank(a.priority) - priorityRank(b.priority);
    if (pr !== 0) return pr;
    const ad = a.due ?? "￿"; // no due date sorts last
    const bd = b.due ?? "￿";
    return ad < bd ? -1 : ad > bd ? 1 : 0;
  }
  function sorted(g: TaskGroup): TaskGroup {
    if (g.status === "done" || g.status === "Done") return g;
    return { status: g.status, tasks: [...g.tasks].sort(byUrgency) };
  }

  let {
    canEdit = false,
    statuses = null,
    showAdd = false,
    view = "status",
    buckets: scopeBuckets = null,
    emptyText = "",
  }: {
    canEdit?: boolean;
    statuses?: string[] | null;
    showAdd?: boolean;
    /** "status" groups by doing/open/blocked/done; "agenda" groups by due date. */
    view?: "status" | "agenda";
    /** When set (agenda view), keep only these date buckets — e.g. Today's cockpit. */
    buckets?: string[] | null;
    /** Replaces the default "No tasks yet" lead when the (scoped) list is empty. */
    emptyText?: string;
  } = $props();

  const qc = useQueryClient();
  const tasksQ = createQuery({ queryKey: ["tasks"], queryFn: api.tasks });

  // Per-row writes (done/reopen/status/delete) live in TaskRow; this component
  // owns only the add form. They share the ["tasks"] cache, so both stay in sync.
  const set = (d: { groups: TaskGroup[] }) => qc.setQueryData(["tasks"], d);
  const addMut = createMutation({ mutationFn: api.taskNew, onSuccess: set });

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
  // A due value may carry a time-of-day ("2026-06-08T17:00"); dateOf gives the
  // date part for bucketing into the agenda groups below.
  const dateOf = (due?: string) => (due ? due.slice(0, 10) : "");

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
      .filter(([status, tasks]) => tasks.length > 0 && (!scopeBuckets || scopeBuckets.includes(status)))
      .map(([status, tasks]) => ({ status, tasks }));
  });

  const groups = $derived((view === "agenda" ? agendaGroups : statusGroups).map(sorted));

  function add(e: SubmitEvent) {
    e.preventDefault();
    const t = newText.trim();
    if (t) {
      $addMut.mutate(t);
      newText = "";
    }
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
          <TaskRow {t} {canEdit} />
        {/each}
      </ul>
      {#if group.tasks.length === 0}<p class="muted small">none</p>{/if}
    </section>
  {:else}
    <div class="empty">
      <p class="empty__lead">{emptyText || "No tasks yet."}</p>
      {#if !emptyText}
        <p class="muted">
          Capture one above, or run <code>nt mcp install</code> so your AI agent can add and
          complete tasks through <code>nt_add</code> / <code>nt_done</code> — they share this same list.
        </p>
      {/if}
    </div>
  {/each}
{/if}
