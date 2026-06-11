<script lang="ts">
  import { createQuery, createMutation, useQueryClient } from "@tanstack/svelte-query";
  import { api, type TaskGroup, type Task } from "./api";
  import TaskRow from "./TaskRow.svelte";
  import { priorityRank, priorityClass } from "./text";
  import { parseQuickAdd } from "./quickparse";
  import { taskMatcher } from "./taskfilter";
  import { stepId } from "./listnav";
  import { palette } from "./palette.svelte";
  import { shortcuts, isTextEntry } from "./keys.svelte";

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
    viewName = "",
    filter = "",
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
    /** Client-side quick filter (@tag +project !pri words); "" = show all. */
    filter?: string;
    /** A saved smart view to recall — the server filters/sorts (view.Apply) and
     *  returns one pre-ordered group, rendered as-is (no client re-bucketing). */
    viewName?: string;
  } = $props();

  const qc = useQueryClient();
  const tasksQ = createQuery(
    viewName
      ? { queryKey: ["tasks-view", viewName], queryFn: () => api.tasksView(viewName) }
      : { queryKey: ["tasks"], queryFn: api.tasks },
  );

  // Per-row writes (done/reopen/status/delete) live in TaskRow; this component
  // owns only the add form. They share the ["tasks"] cache, so both stay in sync.
  const set = (d: { groups: TaskGroup[] }) => qc.setQueryData(["tasks"], d);
  const addMut = createMutation({ mutationFn: api.taskNew, onSuccess: set });

  let newText = $state("");
  // Live "here's what I understood" preview of the todo.txt shorthand, so the
  // power syntax is discoverable instead of hidden behind a placeholder.
  const preview = $derived(newText.trim() ? parseQuickAdd(newText) : null);
  const hasMeta = $derived(
    !!preview &&
      (!!preview.priority ||
        !!preview.due ||
        !!preview.start ||
        !!preview.recur ||
        !!preview.est ||
        !!preview.project ||
        preview.tags.length > 0 ||
        preview.links.length > 0),
  );

  // The live quick-filter (W12): client-side, same shorthand as quick-add
  // (@tag +project !pri words). Applied to every group before bucketing/sorting
  // so it works in agenda, status, and saved-view layouts alike.
  const matcher = $derived(taskMatcher(filter));
  const allGroups = $derived.by((): TaskGroup[] => {
    const raw = ($tasksQ.data?.groups ?? []) as TaskGroup[];
    if (matcher.empty) return raw;
    return raw
      .map((g) => ({ status: g.status, tasks: g.tasks.filter(matcher.match) }))
      .filter((g) => g.tasks.length > 0);
  });

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

  // A saved view's group arrives pre-filtered and pre-ordered from the server —
  // render it untouched (re-sorting here would override the view's own sort).
  const groups = $derived(
    viewName ? allGroups : (view === "agenda" ? agendaGroups : statusGroups).map(sorted),
  );

  // ---- j/k row navigation -------------------------------------------------
  // Roving focus over every rendered row, in visual order. The cursor lives in
  // the DOM (document.activeElement), so it survives re-renders and never goes
  // stale; stepId() just maps "which row id is focused" → "the next one".
  const flatIds = $derived(groups.flatMap((g) => g.tasks.map((t) => t.id)));

  function focusedRowId(): string | null {
    const el = document.activeElement;
    const id = el instanceof HTMLElement ? el.id : "";
    return id.startsWith("trow-") ? id.slice(5) : null;
  }
  function moveFocus(dir: 1 | -1) {
    const next = stepId(flatIds, focusedRowId(), dir);
    if (!next) return;
    const el = document.getElementById(`trow-${next}`);
    el?.focus();
    el?.scrollIntoView({ block: "nearest" });
  }
  function onListKey(e: KeyboardEvent) {
    // Don't steal j/k while typing or when a modal owns the keyboard.
    if (e.metaKey || e.ctrlKey || e.altKey || isTextEntry(e.target) || palette.open || shortcuts.open)
      return;
    if (e.key === "j") {
      e.preventDefault();
      moveFocus(1);
    } else if (e.key === "k") {
      e.preventDefault();
      moveFocus(-1);
    }
  }
  $effect(() => {
    window.addEventListener("keydown", onListKey);
    return () => window.removeEventListener("keydown", onListKey);
  });

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
  {#if preview && hasMeta}
    <div class="qa" aria-live="polite">
      {#if preview.priority}<span
          class="pri pri--{priorityClass(preview.priority)}"
          title={`Priority ${preview.priority}`}>{preview.priority}</span
        >{/if}
      <span class="qa__title">{preview.title || "(no title yet)"}</span>
      {#if preview.due}<span class="qa__chip qa__chip--due" title="Due date">due {preview.due}</span>{/if}
      {#if preview.start}<span class="qa__chip" title="Start / defer date">start {preview.start}</span>{/if}
      {#if preview.recur}<span class="qa__chip" title="Repeats">↻ {preview.recur}</span>{/if}
      {#if preview.est}<span class="qa__chip" title="Estimate">est {preview.est}</span>{/if}
      {#if preview.project}<span class="qa__chip qa__chip--proj">+{preview.project}</span>{/if}
      {#each preview.tags as tag (tag)}<span class="qa__chip qa__chip--tag">@{tag}</span>{/each}
      {#each preview.links as link (link)}<span class="qa__chip qa__chip--link">[[{link}]]</span>{/each}
    </div>
  {/if}
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

<style>
  /* Live parse preview under the quick-add box: a calm strip that mirrors how the
     task will look once added, so the todo.txt shorthand is discoverable. */
  .qa {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    margin: -4px 0 14px;
    padding: 6px 10px;
    background: var(--bg-inset);
    border-radius: var(--radius-sm);
    font-size: 0.85rem;
  }
  .qa__title {
    color: var(--fg);
    font-weight: 500;
  }
  .qa__chip {
    font-size: 0.72rem;
    color: var(--fg-soft);
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0 7px;
  }
  .qa__chip--due {
    color: var(--accent-2);
  }
  .qa__chip--proj,
  .qa__chip--link {
    color: var(--accent);
  }
  .qa__chip--tag {
    color: var(--accent-2);
  }
</style>
