<script lang="ts">
  import { createQuery, createMutation, useQueryClient } from "@tanstack/svelte-query";
  import { api, type TaskGroup, type Task } from "./api";
  import TaskRow from "./TaskRow.svelte";
  import Icon from "./Icon.svelte";
  import { priorityRank, priorityClass, dueTier } from "./text";
  import { parseQuickAdd } from "./quickparse";
  import { taskMatcher } from "./taskfilter";
  import { stepId } from "./listnav";
  import { palette } from "./palette.svelte";
  import { shortcuts, isTextEntry } from "./keys.svelte";
  import { showToast, clearUndoToast } from "./toast.svelte";

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
    statuses = null,
    showAdd = false,
    view = "status",
    buckets: scopeBuckets = null,
    emptyText = "",
    viewName = "",
    filter = "",
  }: {
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
  const addMut = createMutation({
    mutationFn: api.taskNew,
    // A new task is a fresh write — a stale Undo from a prior op would now revert
    // the wrong thing, so clear it (W10).
    onSuccess: (d) => {
      clearUndoToast();
      set(d);
    },
    onError: (e) => showToast(`Couldn't add task: ${String(e)}`),
  });

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
        preview.links.length > 0 ||
        preview.emptyKeys.length > 0),
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

  // A due value may carry a time-of-day ("2026-06-08T17:00"); dateOf gives the
  // date part for bucketing into the agenda groups below.
  const dateOf = (due?: string) => (due ? due.slice(0, 10) : "");

  // Agenda view: re-bucket every task by its due date (the planner layout). The
  // over/today/soon/later split comes from the SHARED dueTier() (week horizon =
  // 7d here) so the agenda can never drift from the row/board temperature — see
  // lib/text.ts (finding 16). Same semantics as the old inline comparison.
  const agendaGroups = $derived.by((): TaskGroup[] => {
    const buckets = {
      Overdue: [] as Task[],
      Today: [] as Task[],
      "This week": [] as Task[],
      Later: [] as Task[],
      "No date": [] as Task[],
      Done: [] as Task[],
    };
    const TIER_BUCKET = { over: "Overdue", today: "Today", soon: "This week", later: "Later" } as const;
    for (const g of allGroups) {
      for (const t of g.tasks) {
        const due = dateOf(t.due); // YYYY-MM-DD, ignoring any time-of-day suffix
        if (t.status === "done") buckets.Done.push(t);
        else if (!due) buckets["No date"].push(t);
        else buckets[TIER_BUCKET[dueTier(due, 7)]].push(t);
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
  // After a row leaves the list (completed/deleted), move roving focus to the row
  // that took its slot — the next sibling in the OLD order, or the previous one if
  // it was last (W12). Captured from flatIds *before* the write, so we know who
  // followed; deferred a microtask so the post-write re-render has settled.
  function focusSibling(removedId: string) {
    const idx = flatIds.indexOf(removedId);
    const after = flatIds.slice(idx + 1);
    const before = flatIds.slice(0, idx).reverse();
    queueMicrotask(() => {
      for (const id of [...after, ...before]) {
        const el = document.getElementById(`trow-${id}`);
        if (el) {
          el.focus();
          el.scrollIntoView({ block: "nearest" });
          return;
        }
      }
    });
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
    } else if (e.key === "Escape" && selected.length > 0) {
      e.preventDefault();
      selected = [];
    }
  }
  $effect(() => {
    window.addEventListener("keydown", onListKey);
    return () => window.removeEventListener("keydown", onListKey);
  });

  // ---- bulk selection (W11) ----------------------------------------------
  // x selects the focused row (handled in TaskRow); once a selection exists, a
  // bar offers complete / reschedule / delete across the whole set. Selection is
  // a plain id array (reactive in runes) and is pruned to what's still visible
  // so an action never touches a row that scrolled out of the current filter.
  let selected = $state<string[]>([]);
  function toggleSel(id: string) {
    selected = selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id];
  }
  const visibleIds = $derived(new Set(flatIds));
  const selCount = $derived(selected.filter((id) => visibleIds.has(id)).length);
  // Prune the selection to what's still visible whenever the list changes (filter
  // edit / SSE refetch), so the bar's count and any action never reference a row
  // that scrolled out of the current view (W18). Only writes when it shrinks.
  $effect(() => {
    const pruned = selected.filter((id) => visibleIds.has(id));
    if (pruned.length !== selected.length) selected = pruned;
  });
  let bulkBusy = $state(false);
  let rescheduling = $state(false);
  let bulkMenuEl: HTMLElement | undefined = $state();
  let bulkMenuRestore: HTMLElement | null = null;

  const BULK_DUE = [
    { label: "Today", value: "today" },
    { label: "Tomorrow", value: "tomorrow" },
    { label: "Next week", value: "+7d" },
    { label: "No date", value: "none" },
  ];

  // Mirror TaskRow's reschedule menu: focus the first item on open, restore
  // focus to the trigger on close, and let Arrow/Esc drive it (role="menu").
  function openBulkMenu() {
    bulkMenuRestore = document.activeElement as HTMLElement;
    rescheduling = true;
    queueMicrotask(() => bulkMenuEl?.querySelector("button")?.focus());
  }
  function closeBulkMenu() {
    rescheduling = false;
    bulkMenuRestore?.focus?.();
    bulkMenuRestore = null;
  }
  function onBulkMenuKey(e: KeyboardEvent) {
    e.stopPropagation(); // the list's j/k handler must never see menu keys
    if (e.key === "Escape") {
      e.preventDefault();
      closeBulkMenu();
    } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      const items = [...(bulkMenuEl?.querySelectorAll("button") ?? [])];
      const i = items.indexOf(document.activeElement as HTMLButtonElement);
      const n = e.key === "ArrowDown" ? (i + 1) % items.length : (i - 1 + items.length) % items.length;
      items[n]?.focus();
    }
  }

  const refreshAll = () => {
    for (const k of [["tasks"], ["tasks-view"], ["review"], ["state"], ["activity"]]) {
      qc.invalidateQueries({ queryKey: k });
    }
  };
  // One server call applies the action to the whole set in a SINGLE transaction,
  // so the toast's Undo reverts the entire batch with one api.undo() (the engine
  // is single-level — N separate writes couldn't be unwound as a group).
  async function runBulk(action: "done" | "delete" | "due", verb: string, due = "") {
    const ids = selected.filter((id) => visibleIds.has(id));
    if (!ids.length || bulkBusy) return;
    bulkBusy = true;
    const n = ids.length;
    try {
      await api.taskBulk(action, ids, due);
      clearUndoToast(); // fresh write — drop any stale single-level Undo (W10)
      showToast(`${verb} ${n} task${n > 1 ? "s" : ""}`, async () => {
        try {
          await api.undo();
          refreshAll();
          showToast("Undone");
        } catch (e) {
          showToast(`Couldn't undo: ${String(e)}`);
        }
      });
    } catch (e) {
      showToast(`Couldn't ${verb.toLowerCase()}: ${String(e)}`);
    } finally {
      refreshAll();
      selected = [];
      rescheduling = false;
      bulkBusy = false;
    }
  }
  const bulkDone = () => runBulk("done", "Completed");
  const bulkDelete = () => runBulk("delete", "Deleted");
  const bulkDue = (v: string) => runBulk("due", "Rescheduled", v);

  function add(e: SubmitEvent) {
    e.preventDefault();
    const t = newText.trim();
    if (t) {
      $addMut.mutate(t);
      newText = "";
    }
  }

  // Tint a group header by what the bucket means. In the agenda layout the
  // status IS a date bucket, so "Overdue" runs hot and "Today" takes the accent
  // — the same temperature language the due chips speak. Status/saved-view
  // groups (open/doing/blocked/…) don't map to a temperature, so they stay
  // neutral. Returns a modifier suffix or "" for the default mono header.
  function headerTone(status: string): "" | "over" | "today" {
    if (status === "Overdue") return "over";
    if (status === "Today") return "today";
    return "";
  }
</script>

{#if showAdd}
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
      {#each preview.emptyKeys as k (k)}<span class="qa__chip qa__chip--hint" title="This key needs a value">{k}: needs a value</span>{/each}
    </div>
  {/if}
{/if}

{#if $tasksQ.isPending}
  <p class="muted">Loading tasks…</p>
{:else if $tasksQ.error}
  <div class="loaderr" role="alert">
    <p class="loaderr__msg">Couldn't load tasks.</p>
    <button class="btn btn--ghost btn--sm" onclick={() => $tasksQ.refetch()}>Try again</button>
  </div>
{:else}
  {#each groups as group (group.status)}
    {@const tone = headerTone(group.status)}
    <section class="group">
      <h2 class="group__title {tone ? `group__title--${tone}` : ''}">
        <span class="group__name">{group.status}</span>
        <span class="group__count">{group.tasks.length}</span>
      </h2>
      <ul class="rows">
        {#each group.tasks as t (t.id)}
          <TaskRow
            {t}
            selected={selected.includes(t.id)}
            onToggleSelect={() => toggleSel(t.id)}
            {focusSibling}
          />
        {/each}
      </ul>
      {#if group.tasks.length === 0}<p class="muted small">none</p>{/if}
    </section>
  {:else}
    <div class="empty empty--hero">
      <div class="empty__art" class:empty__art--onboard={!emptyText} aria-hidden="true">
        {#if emptyText}
          <Icon name="check" size={28} strokeWidth={2} />
        {:else}
          <!-- Spectral knowledge-graph motif (the brand mark) for the onboarding
               hero — a gentle nod that tasks + notes share one linked store. -->
          <span class="empty__halo"></span>
          <Icon name="graph" size={30} strokeWidth={1.6} />
        {/if}
      </div>
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

{#if selCount > 0}
  <div class="bulk" role="region" aria-label="Bulk actions">
    <span class="bulk__count">{selCount} selected</span>
    <button class="bulk__btn" disabled={bulkBusy} onclick={bulkDone}>Complete</button>
    <div class="bulk__resched">
      <button
        class="bulk__btn"
        disabled={bulkBusy}
        aria-haspopup="menu"
        aria-expanded={rescheduling}
        onclick={() => (rescheduling ? closeBulkMenu() : openBulkMenu())}
        >Reschedule <Icon name="chevron-down" size={13} /></button
      >
      {#if rescheduling}
        <div class="bulk__backdrop" role="presentation" onclick={closeBulkMenu}></div>
        <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
        <div
          class="bulk__menu"
          role="menu"
          aria-label="Reschedule selected tasks"
          tabindex="-1"
          bind:this={bulkMenuEl}
          onkeydown={onBulkMenuKey}
        >
          {#each BULK_DUE as p (p.value)}
            <button role="menuitem" class="bulk__item" onclick={() => bulkDue(p.value)}>{p.label}</button>
          {/each}
        </div>
      {/if}
    </div>
    <button class="bulk__btn bulk__btn--danger" disabled={bulkBusy} onclick={bulkDelete}>Delete</button>
    <button class="bulk__btn bulk__btn--ghost" onclick={() => (selected = [])}>Clear (esc)</button>
  </div>
{/if}

<style>
  /* Group header: the mono microlabel (from app.css) gets a tabular count chip
     and an optional temperature tone for the agenda's date buckets. */
  .group__title {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .group__name {
    display: inline-flex;
    align-items: center;
  }
  /* Count chip — tabular mono in a calm inset pill, so the bucket size reads as
     metadata, not a heading word. */
  .group__count {
    font-size: var(--text-footnote);
    font-variant-numeric: tabular-nums;
    color: var(--label-secondary);
    background: var(--bg-inset);
    border-radius: 999px;
    padding: 0 6px;
    line-height: 1.6;
  }
  /* Agenda date buckets speak the due-temperature language: overdue runs hot,
     today takes the accent. Colour sits on the content surface (AA in both
     themes); a leading tick of colour anchors it. */
  .group__title--over .group__name {
    color: var(--red);
  }
  .group__title--over .group__count {
    color: var(--red);
    background: color-mix(in srgb, var(--red) 11%, transparent);
  }
  .group__title--today .group__name {
    color: var(--accent-color);
  }
  .group__title--today .group__count {
    color: var(--accent-color);
    background: var(--accent-tint);
  }

  /* Onboarding empty hero: a soft spectral halo blooming behind the graph mark,
     so the brand motif glows without any text sitting on the gradient. */
  .empty__halo {
    position: absolute;
    width: 64px;
    height: 64px;
    border-radius: 50%;
    background: var(--grad-spectral);
    opacity: 0.22;
    filter: blur(11px);
    z-index: 0;
  }
  .empty__art {
    position: relative; /* contain the halo */
    overflow: visible;
  }
  .empty__art :global(.icon) {
    position: relative;
    z-index: 1;
  }

  /* Bulk action bar (W11): a sticky footer that appears once rows are selected. */
  .bulk {
    position: sticky;
    bottom: 12px;
    z-index: 30;
    display: flex;
    align-items: center;
    flex-wrap: wrap; /* finding 8: never overflow on a narrow viewport */
    max-width: 100%;
    gap: 8px;
    margin-top: 16px;
    padding: 8px 12px;
    background: color-mix(in srgb, var(--bg-elevated) 82%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    border-radius: var(--radius);
    box-shadow: var(--shadow-float), var(--glass-hairline);
  }
  /* Finding 8: under ~640px the count + four buttons overflow ~375px screens.
     Wrapping (above) handles the worst case; here we also tighten the buttons so
     the bar usually still fits one row, and flip the Reschedule popover to the
     right edge so it can't clip off-screen. */
  @media (max-width: 640px) {
    .bulk {
      gap: 6px;
    }
    .bulk__count {
      flex-basis: 100%; /* count on its own line; the actions share the next */
      margin-right: 0;
    }
    .bulk__btn {
      padding: 4px 9px;
      font-size: 0.8rem;
    }
    .bulk__menu {
      left: auto;
      right: 0;
      transform-origin: bottom right;
    }
  }
  .bulk__count {
    font-family: var(--font-mono);
    font-size: var(--text-callout);
    font-variant-numeric: tabular-nums;
    color: var(--label-secondary);
    margin-right: 4px;
  }
  .bulk__btn {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    min-height: 28px;
    background: var(--fill);
    border: 0.5px solid var(--separator-strong);
    border-radius: var(--radius-sm);
    color: var(--fg);
    cursor: pointer;
    padding: 4px 12px;
    font-size: 0.85rem;
    transition:
      background var(--motion-fast) var(--ease),
      color var(--motion-fast) var(--ease),
      border-color var(--motion-fast) var(--ease),
      transform var(--motion-fast) var(--ease);
  }
  .bulk__btn:hover:not(:disabled) {
    background: var(--fill-strong);
    border-color: color-mix(in srgb, var(--separator-strong) 55%, var(--fg-soft));
  }
  .bulk__btn:active:not(:disabled) {
    background: var(--fill-active);
    transform: scale(0.96);
  }
  .bulk__btn:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .bulk__btn--danger:hover:not(:disabled) {
    border-color: var(--red);
    color: var(--red);
  }
  .bulk__btn--ghost {
    background: none;
    border-color: transparent;
    color: var(--muted);
    margin-left: auto;
  }
  .bulk__btn--ghost:hover:not(:disabled) {
    background: var(--fill-hover);
    color: var(--fg);
  }
  .bulk__resched {
    position: relative;
  }
  /* Transparent full-viewport catcher so any outside click dismisses the menu. */
  .bulk__backdrop {
    position: fixed;
    inset: 0;
    z-index: 1;
  }
  .bulk__menu {
    position: absolute;
    left: 0;
    bottom: calc(100% + 6px);
    z-index: 2;
    display: flex;
    flex-direction: column;
    min-width: 130px;
    padding: 4px;
    background: color-mix(in srgb, var(--bg-elevated) 90%, transparent);
    -webkit-backdrop-filter: blur(20px) saturate(170%);
    backdrop-filter: blur(20px) saturate(170%);
    border: 0.5px solid var(--separator);
    border-radius: var(--radius-popover);
    box-shadow: var(--shadow-popover);
    transform-origin: bottom left;
    animation: popover-in var(--motion) var(--ease-spring);
  }
  .bulk__menu:focus {
    outline: none;
  }
  .bulk__item {
    text-align: left;
    background: none;
    border: none;
    color: var(--fg);
    padding: 5px 9px;
    border-radius: var(--radius-xs);
    cursor: pointer;
    font-size: 0.85rem;
    transition: background var(--motion-fast) var(--ease);
  }
  /* Arrow-key focus is programmatic (:focus, not :focus-visible); highlight the
     active item directly, with the same calm fill on hover. */
  .bulk__item:hover,
  .bulk__item:focus {
    background: var(--fill-hover);
    outline: none;
  }
  .bulk__item:focus-visible {
    background: var(--accent-fill);
    color: var(--on-accent);
  }
  /* Friendly load-failure state — never expose the raw error object. */
  .loaderr {
    display: flex;
    align-items: center;
    gap: 12px;
    margin: 28px 0;
  }
  .loaderr__msg {
    margin: 0;
    color: var(--fg-soft);
  }
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
    border: 0.5px solid var(--separator-strong);
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
  /* A recognised key typed with no value yet — a quiet amber nudge, not an error.
     The fallback is a real warning amber (not --accent-2 teal) so the warning
     reads even before the --orange token lands, and stays distinct from the teal
     due/tag chips (finding 9). */
  .qa__chip--hint {
    color: var(--orange, #a8620a);
    border-color: color-mix(in srgb, currentColor 45%, transparent);
  }
</style>
