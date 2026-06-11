<script lang="ts">
  // The one task row, used everywhere a task is shown (Tasks, Review, Today) so a
  // task looks — and behaves — identically regardless of which view surfaced it.
  // It owns its writes: after any change it pushes the fresh groups into the
  // ["tasks"] cache and invalidates the derived task views (review, counts,
  // activity) so every surface stays consistent from a single click.
  import { createMutation, useQueryClient } from "@tanstack/svelte-query";
  import { api, type Task, type TaskGroup } from "./api";
  import { priorityClass, relativeDue, meaningfulSource } from "./text";

  let { t, canEdit = false }: { t: Task; canEdit?: boolean } = $props();

  const qc = useQueryClient();
  const synced = (d: { groups: TaskGroup[] }) => {
    qc.setQueryData(["tasks"], d); // instant in any view subscribed to the task list
    for (const k of [["review"], ["state"], ["activity"]]) {
      qc.invalidateQueries({ queryKey: k });
    }
  };
  const doneMut = createMutation({ mutationFn: api.taskDone, onSuccess: synced });
  const reopenMut = createMutation({ mutationFn: api.taskReopen, onSuccess: synced });
  const statusMut = createMutation({
    mutationFn: (a: { id: string; status: string }) => api.taskStatus(a.id, a.status),
    onSuccess: synced,
  });
  const deleteMut = createMutation({ mutationFn: api.taskDelete, onSuccess: synced });
  const editMut = createMutation({
    mutationFn: (a: { id: string; text: string }) => api.taskEdit(a.id, a.text),
    onSuccess: synced,
  });

  // Inline edit of the task text — also the way to read a long title in full
  // (the list clamps it; the editor shows everything).
  let editing = $state(false);
  let draft = $state("");
  function startEdit() {
    draft = t.text;
    editing = true;
  }
  function saveEdit() {
    const v = draft.trim();
    if (v && v !== t.text) $editMut.mutate({ id: t.id, text: v });
    editing = false;
  }
  function onEditKey(e: KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      saveEdit();
    } else if (e.key === "Escape") {
      e.preventDefault();
      editing = false;
    }
  }
  // Focus the editor, drop the cursor at the end, and grow it to fit the text.
  function autoedit(node: HTMLTextAreaElement) {
    const grow = () => {
      node.style.height = "auto";
      node.style.height = node.scrollHeight + "px";
    };
    node.focus();
    node.setSelectionRange(node.value.length, node.value.length);
    grow();
    node.addEventListener("input", grow);
    return { destroy: () => node.removeEventListener("input", grow) };
  }

  // Priority colour cue (A/B/C/rest → "") and a human-friendly due label that
  // also tells us whether the task is overdue or due soon, all from one place so
  // every surface renders a task identically.
  const pri = $derived(priorityClass(t.priority));
  const due = $derived(t.due ? relativeDue(t.due) : null);
  // Only surface a source badge for non-default origins (chiefly an AI agent).
  const src = $derived(meaningfulSource(t.source));

  function toggleDoing() {
    $statusMut.mutate({ id: t.id, status: t.status === "doing" ? "open" : "doing" });
  }
  function del() {
    if (confirm(`Delete task "${t.text}"? (undoable with nt undo)`)) $deleteMut.mutate(t.id);
  }

  // Keyboard actions on the focused row (j/k focus is driven by TaskRows). Space
  // or Enter toggles done/reopen; `e` opens the inline editor. j/k are left to
  // bubble up to the list navigator.
  function onRowKey(e: KeyboardEvent) {
    if (editing || !canEdit) return;
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      if (t.status === "done") $reopenMut.mutate(t.id);
      else $doneMut.mutate(t.id);
    } else if (e.key === "e") {
      if (t.status === "done") return;
      e.preventDefault();
      startEdit();
    }
  }
</script>

<!-- A row is a roving-focus target for j/k list nav: focusable (tabindex -1, not in
     the Tab order) with Space/e shortcuts; the real controls stay separate buttons. -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
<li
  id={`trow-${t.id}`}
  class="row {pri ? `row--pri-${pri}` : ''}"
  class:row--doing={t.status === "doing"}
  class:row--editing={editing}
  tabindex="-1"
  onkeydown={onRowKey}
>
  {#if pri && t.status !== "done"}
    <span class="pri pri--{pri}" title={`Priority ${t.priority}`} aria-label={`Priority ${t.priority}`}
      >{t.priority}</span
    >
  {/if}
  {#if canEdit}
    {#if t.status === "done"}
      <button class="check check--done" title="Reopen" aria-label="Reopen task" onclick={() => $reopenMut.mutate(t.id)}>●</button>
    {:else}
      <button class="check" title="Mark done" aria-label="Mark task done" onclick={() => $doneMut.mutate(t.id)}>○</button>
    {/if}
  {/if}
  {#if editing}
    <textarea
      class="row__edit"
      bind:value={draft}
      rows="1"
      aria-label="Edit task text"
      use:autoedit
      onkeydown={onEditKey}
    ></textarea>
    <span class="row__actions row__actions--shown">
      <button class="rowbtn" title="Save (↵)" aria-label="Save" onclick={saveEdit}>✓</button>
      <button class="rowbtn rowbtn--danger" title="Cancel (esc)" aria-label="Cancel" onclick={() => (editing = false)}>×</button>
    </span>
  {:else}
    <span class="row__text" class:done={t.status === "done"} title={t.text}>{t.text}</span>
    {#if t.recur}<span class="row__recur" title="Recurring task">↻</span>{/if}
    {#if t.status === "doing"}<span class="status-pill status-pill--doing">doing</span>{/if}
    {#if t.status === "blocked"}<span class="status-pill status-pill--blocked" title={t.blocker ? `blocked by: ${t.blocker}` : "blocked"}>⊘ blocked</span>{/if}
    {#if t.project}<a class="chip" href={`/search?tag=${encodeURIComponent(t.project)}`}>+{t.project}</a>{/if}
    {#each t.tags ?? [] as tag (tag)}<a class="chip chip--tag" href={`/search?tag=${encodeURIComponent(tag)}`}>@{tag}</a>{/each}
    {#if due}<span
        class="row__due"
        class:row__due--over={due.overdue && t.status !== "done"}
        class:row__due--soon={due.soon && t.status !== "done"}
        title={t.due}>{due.label}</span
      >{/if}
    {#if src}<span class="src src--agent" title={`Captured by ${src}`}>{src}</span>{/if}
    {#if canEdit && t.status !== "done"}
      <span class="row__actions">
        <button class="rowbtn" title="Edit text" aria-label="Edit task text" onclick={startEdit}>✎</button>
        <button class="rowbtn" title={t.status === "doing" ? "Stop (set open)" : "Start (set doing)"} onclick={toggleDoing}>◐</button>
        <button class="rowbtn rowbtn--danger" title="Delete (undoable)" onclick={del}>×</button>
      </span>
    {/if}
  {/if}
</li>

<style>
  /* Center the check/text/metadata (the global .row uses baseline, for prose
     lists); every task row shares this so Tasks, Review, and Today line up. */
  .row {
    align-items: center;
    gap: 8px;
  }
  /* Roving keyboard focus (j/k). Programmatic focus, so we style :focus directly
     rather than :focus-visible (which can skip scripted focus). */
  .row:focus {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
    border-radius: var(--radius-sm);
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
  .row__actions--shown {
    opacity: 1; /* save/cancel stay visible while the row is in edit mode */
  }
  .row--editing {
    align-items: flex-start; /* the editor can be multi-line; top-align the check */
  }
  .row__edit {
    flex: 1;
    min-width: 0;
    font: inherit;
    line-height: 1.45;
    resize: none;
    overflow: hidden;
    padding: 3px 8px;
    background: var(--bg-elev);
    border: 1px solid var(--accent);
    border-radius: var(--radius-sm);
    color: var(--fg);
  }
  .row__edit:focus {
    outline: none;
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
  /* Priority left accent bar (the .pri letter chip itself is a global primitive
     in app.css). Red is reserved for A and overdue, so it stays meaningful. */
  .row--pri-a {
    box-shadow: inset 2px 0 0 var(--pri-a);
  }
  .row--pri-b {
    box-shadow: inset 2px 0 0 var(--pri-b);
  }
  .row--pri-c {
    box-shadow: inset 2px 0 0 var(--pri-c);
  }
  .row--pri-a,
  .row--pri-b,
  .row--pri-c,
  .row--doing {
    padding-left: 8px;
    margin-left: -8px;
  }
  /* The "doing" accent wins over the priority bar when both apply. */
  .row--doing {
    box-shadow: inset 2px 0 0 var(--accent);
  }
  .row__due {
    color: var(--muted);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .row__due--soon {
    color: var(--fg-soft);
    font-weight: 600;
  }
  .row__due--over {
    color: var(--red);
    font-weight: 600;
  }
  /* Agent-captured tasks (e.g. src:claude) — nt's reason to exist; quiet, but
     legible, so you can tell what your AI session added. */
  .src--agent {
    color: var(--accent);
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    padding: 0 5px;
    border-radius: 999px;
    font-size: 0.7rem;
  }
</style>
