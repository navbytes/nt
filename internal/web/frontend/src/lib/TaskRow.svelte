<script lang="ts">
  // The one task row, used everywhere a task is shown (Tasks, Review, Today) so a
  // task looks — and behaves — identically regardless of which view surfaced it.
  // It owns its writes: after any change it pushes the fresh groups into the
  // ["tasks"] cache and invalidates the derived task views (review, counts,
  // activity) so every surface stays consistent from a single click.
  import { createMutation, useQueryClient } from "@tanstack/svelte-query";
  import { api, type Task, type TaskGroup } from "./api";

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

  function todayISO(): string {
    const d = new Date();
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
  }
  // A due value may carry a time-of-day ("2026-06-08T17:00"); dateOf gives the
  // date part for the overdue test, fmtDue renders it readably.
  const dateOf = (due?: string) => (due ? due.slice(0, 10) : "");
  const fmtDue = (due: string) => (due.includes("T") ? due.replace("T", " ") : due);

  function toggleDoing() {
    $statusMut.mutate({ id: t.id, status: t.status === "doing" ? "open" : "doing" });
  }
  function del() {
    if (confirm(`Delete task "${t.text}"? (undoable with nt undo)`)) $deleteMut.mutate(t.id);
  }
</script>

<li class="row" class:row--doing={t.status === "doing"} class:row--editing={editing}>
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
    {#if t.due}<span class="row__due" class:row__due--over={dateOf(t.due) < todayISO() && t.status !== "done"}>{fmtDue(t.due)}</span>{/if}
    {#if t.source}<span class="src">{t.source}</span>{/if}
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
  .row__due--over {
    color: var(--red);
    font-weight: 600;
  }
</style>
