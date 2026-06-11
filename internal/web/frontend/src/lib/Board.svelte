<script lang="ts">
  import { createQuery, createMutation, useQueryClient } from "@tanstack/svelte-query";
  import { api, type TaskGroup, type Task } from "./api";
  import { showToast } from "./toast.svelte";
  import { displayTitle } from "./text";

  let { canEdit = false }: { canEdit?: boolean } = $props();

  const qc = useQueryClient();
  const tasksQ = createQuery({ queryKey: ["tasks"], queryFn: api.tasks });
  const set = (d: { groups: TaskGroup[] }) => qc.setQueryData(["tasks"], d);
  async function undoLast() {
    try {
      set(await api.undo());
      showToast("Undone");
    } catch (e) {
      showToast(`Couldn't undo: ${String(e)}`);
    }
  }
  const doneMut = createMutation({ mutationFn: api.taskDone, onSuccess: set });
  const deleteMut = createMutation({ mutationFn: api.taskDelete, onSuccess: set });
  const statusMut = createMutation({
    mutationFn: (a: { id: string; status: string }) => api.taskStatus(a.id, a.status),
    onSuccess: set,
  });
  // No confirm() interrogation (and webviews don't implement it anyway): delete
  // acts immediately and the toast offers Undo — same contract as the list rows.
  function del(t: Task) {
    $deleteMut.mutate(t.id, {
      onSuccess: () => showToast(`Deleted “${displayTitle(t.text, 32)}”`, undoLast),
    });
  }

  // Columns left→right. The column IS the task's status — a drop is just a
  // status write, so nothing positional needs storing (the whole point).
  const COLUMNS = [
    { key: "open", label: "Open" },
    { key: "doing", label: "Doing" },
    { key: "blocked", label: "Blocked" },
    { key: "done", label: "Done" },
  ];
  const DONE_CAP = 12; // the done pile grows unbounded; show the most recent

  const byStatus = $derived.by((): Record<string, Task[]> => {
    const m: Record<string, Task[]> = { open: [], doing: [], blocked: [], done: [] };
    for (const g of ($tasksQ.data?.groups ?? []) as TaskGroup[]) {
      if (g.status in m) m[g.status] = g.tasks;
    }
    return m;
  });
  const colTasks = (k: string): Task[] => byStatus[k] ?? [];

  let dragId = $state("");
  let dragFrom = $state("");
  let overCol = $state("");

  function drop(target: string) {
    const id = dragId,
      from = dragFrom;
    dragId = dragFrom = overCol = "";
    if (!id || from === target) return;
    // Done is the *action* (Complete handles recurrence); every other column is
    // a plain status write that the API also un-dones if needed.
    if (target === "done") $doneMut.mutate(id);
    else $statusMut.mutate({ id, status: target });
  }

  const dateOf = (due?: string) => (due ? due.slice(0, 10) : "");
  const todayISO = () => new Date().toISOString().slice(0, 10);
  const fmtDue = (due: string) => (due.includes("T") ? due.replace("T", " ") : due);
</script>

<div class="board">
  {#each COLUMNS as col (col.key)}
    <section
      class="bcol"
      class:bcol--over={overCol === col.key}
      class:bcol--done={col.key === "done"}
      role="list"
      ondragover={(e) => {
        if (canEdit && dragId) {
          e.preventDefault();
          overCol = col.key;
        }
      }}
      ondragleave={() => {
        if (overCol === col.key) overCol = "";
      }}
      ondrop={(e) => {
        e.preventDefault();
        drop(col.key);
      }}
    >
      <h2 class="bcol__head">{col.label} <span class="bcol__count">{colTasks(col.key).length}</span></h2>
      <div class="bcol__cards">
        {#each col.key === "done" ? colTasks("done").slice(0, DONE_CAP) : colTasks(col.key) as t (t.id)}
          <article
            class="bcard pri-{t.priority || 'none'}"
            class:bcard--dragging={dragId === t.id}
            draggable={canEdit}
            role="listitem"
            title={t.blocker ? `${t.text}\nblocked by: ${t.blocker}` : t.text}
            ondragstart={(e) => {
              dragId = t.id;
              dragFrom = col.key;
              e.dataTransfer?.setData("text/plain", t.id);
            }}
            ondragend={() => {
              dragId = dragFrom = overCol = "";
            }}
          >
            <div class="bcard__top">
              {#if t.priority}<span class="bcard__pri">{t.priority}</span>{/if}
              <span class="bcard__title">{t.text}</span>
            </div>
            <div class="bcard__meta">
              {#if t.recur}<span class="row__recur" title="Recurring task">↻</span>{/if}
              {#if t.project}<a class="chip" href={`/search?tag=${encodeURIComponent(t.project)}`}>+{t.project}</a>{/if}
              {#each (t.tags ?? []).slice(0, 3) as tag (tag)}<a class="chip chip--tag" href={`/search?tag=${encodeURIComponent(tag)}`}>@{tag}</a>{/each}
              {#if (t.tags?.length ?? 0) > 3}<span class="chip chip--more">+{(t.tags?.length ?? 0) - 3}</span>{/if}
              {#if t.due}<span class="row__due" class:row__due--over={dateOf(t.due) < todayISO() && col.key !== "done"}>{fmtDue(t.due)}</span>{/if}
            </div>
            {#if canEdit}
              <div class="bcard__actions">
                {#if col.key !== "done"}<button class="rowbtn" title="Mark done" aria-label="Mark done" onclick={() => $doneMut.mutate(t.id)}>✓</button>{/if}
                <button
                  class="rowbtn rowbtn--danger"
                  title="Delete (undoable)"
                  aria-label="Delete task"
                  onclick={() => del(t)}>×</button>
              </div>
            {/if}
          </article>
        {/each}
        {#if col.key === "done" && colTasks("done").length > DONE_CAP}
          <p class="muted small">+{colTasks("done").length - DONE_CAP} more done</p>
        {/if}
        {#if colTasks(col.key).length === 0}
          <p class="bcol__empty">{canEdit ? "drop here" : "—"}</p>
        {/if}
      </div>
    </section>
  {/each}
</div>

<style>
  .board {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 12px;
    margin-top: 16px;
    align-items: start;
  }
  .bcol {
    background: var(--bg-inset);
    border: 1px solid transparent;
    border-radius: var(--radius);
    padding: 8px;
    min-width: 0;
  }
  .bcol--over {
    border-color: var(--accent);
    border-style: dashed;
  }
  .bcol--done {
    opacity: 0.72;
  }
  .bcol__head {
    font-size: 0.78rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--fg-soft);
    margin: 2px 4px 8px;
    display: flex;
    gap: 6px;
    align-items: baseline;
  }
  .bcol__count {
    color: var(--muted);
    font-family: var(--font-mono);
    font-size: 0.72rem;
  }
  .bcol__cards {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-height: 40px;
  }
  .bcol__empty {
    color: var(--muted);
    font-size: 0.75rem;
    text-align: center;
    padding: 12px 0;
    border: 1px dashed var(--border);
    border-radius: var(--radius-sm);
    margin: 0;
  }
  .bcard {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-left: 3px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 8px 10px;
    cursor: grab;
  }
  .bcard--dragging {
    opacity: 0.4;
  }
  /* Priority color cue, derived from the task's (A..Z) priority — no storage. */
  .bcard.pri-A {
    border-left-color: var(--red);
  }
  .bcard.pri-B {
    border-left-color: #d9892b;
  }
  .bcard.pri-C {
    border-left-color: var(--accent-2);
  }
  .bcard__top {
    display: flex;
    gap: 6px;
    align-items: baseline;
  }
  .bcard__pri {
    flex: none;
    font-family: var(--font-mono);
    font-size: 0.68rem;
    font-weight: 700;
    color: var(--red);
    border: 1px solid currentColor;
    border-radius: 3px;
    padding: 0 3px;
    line-height: 1.3;
  }
  .pri-B .bcard__pri {
    color: #d9892b;
  }
  .pri-C .bcard__pri {
    color: var(--accent-2);
  }
  .bcard__title {
    min-width: 0;
    font-size: 0.86rem;
    line-height: 1.35;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .bcard__meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 5px;
    margin-top: 7px;
  }
  .bcard__actions {
    display: flex;
    gap: 4px;
    justify-content: flex-end;
    margin-top: 6px;
    opacity: 0;
    transition: opacity 0.1s;
  }
  .bcard:hover .bcard__actions {
    opacity: 1;
  }
</style>
