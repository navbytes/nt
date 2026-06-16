<script lang="ts">
  import { createQuery, createMutation, useQueryClient } from "@tanstack/svelte-query";
  import { api, type TaskGroup, type Task } from "./api";
  import { showToast } from "./toast.svelte";
  import { displayTitle, fmtDuration } from "./text";
  import Icon from "./Icon.svelte";

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

  // The one place a card actually changes column. Done is the *action*
  // (Complete handles recurrence); every other column is a plain status write
  // that the API also un-dones if needed. Both the drag-drop and the keyboard
  // "Move to…" select funnel through here so they stay in lockstep.
  function moveTo(id: string, from: string, target: string) {
    if (!id || from === target) return;
    if (target === "done") $doneMut.mutate(id);
    else $statusMut.mutate({ id, status: target });
  }

  function drop(target: string) {
    const id = dragId,
      from = dragFrom;
    dragId = dragFrom = overCol = "";
    moveTo(id, from, target);
  }

  const fmtDue = (due: string) => (due.includes("T") ? due.replace("T", " ") : due);

  // Due-date "temperature" for a card's due chip — the same tiers the list rows
  // speak (overdue→hot, today→accent, soon→teal, later→muted). Date-only diff so
  // a task due today isn't overdue; Done cards never run hot. Local, no API change.
  function dueTier(dueRaw: string, done: boolean): "over" | "today" | "soon" | "later" {
    if (done) return "later";
    const [y, mo, d] = dueRaw.slice(0, 10).split("-").map(Number);
    if (!y || !mo || !d) return "later";
    const now = new Date();
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const diff = Math.round((new Date(y, mo - 1, d).getTime() - today.getTime()) / 86400000);
    if (diff < 0) return "over";
    if (diff === 0) return "today";
    return diff <= 3 ? "soon" : "later";
  }
</script>

<div class="board">
  {#each COLUMNS as col (col.key)}
    <section
      class="bcol"
      class:bcol--over={overCol === col.key}
      class:bcol--doing={col.key === "doing"}
      class:bcol--done={col.key === "done"}
      role="list"
      ondragover={(e) => {
        if (dragId) {
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
            draggable={true}
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
              {#if t.recur}<span class="bcard__recur" title="Recurring task" aria-label="Recurring task"><Icon name="repeat" size={12} /></span>{/if}
              {#if t.project}<a class="chip" href={`/search?tag=${encodeURIComponent(t.project)}`}>+{t.project}</a>{/if}
              {#each (t.tags ?? []).slice(0, 3) as tag (tag)}<a class="chip chip--tag" href={`/search?tag=${encodeURIComponent(tag)}`}>@{tag}</a>{/each}
              {#if (t.tags?.length ?? 0) > 3}<span class="chip chip--more">+{(t.tags?.length ?? 0) - 3}</span>{/if}
              {#if t.est && col.key !== "done"}<span class="bcard__est" title={`Estimated ${fmtDuration(t.est)}`}>{fmtDuration(t.est)}</span>{/if}
              {#if t.due}{@const tier = dueTier(t.due, col.key === "done")}<span class="bcard__due bcard__due--{tier}">{#if tier === "over"}<Icon name="flame" size={10} filled />{/if}{fmtDue(t.due)}</span>{/if}
            </div>
            <div class="bcard__actions">
              <!-- Full-Keyboard-Access move: the board's only way to change a
                   card's column without a mouse drag. Funnels through moveTo()
                   (same as ondrop); resets so it always reads "Move to…". -->
              <select
                class="bcard__move"
                aria-label="Move task to column"
                title="Move to…"
                onchange={(e) => {
                  const target = e.currentTarget.value;
                  e.currentTarget.value = "";
                  moveTo(t.id, col.key, target);
                }}
              >
                <option value="" disabled selected>Move to…</option>
                {#each COLUMNS as c (c.key)}
                  {#if c.key !== col.key}<option value={c.key}>{c.label}</option>{/if}
                {/each}
              </select>
              {#if col.key !== "done"}<button class="rowbtn" title="Mark done" aria-label="Mark done" onclick={() => $doneMut.mutate(t.id)}><Icon name="check" size={14} /></button>{/if}
              <button
                class="rowbtn rowbtn--danger"
                title="Delete (undoable)"
                aria-label="Delete task"
                onclick={() => del(t)}><Icon name="close" size={14} /></button>
            </div>
          </article>
        {/each}
        {#if col.key === "done" && colTasks("done").length > DONE_CAP}
          <p class="muted small">+{colTasks("done").length - DONE_CAP} more done</p>
        {/if}
        {#if colTasks(col.key).length === 0}
          <p class="bcol__empty">drop here</p>
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
    position: relative;
    background: color-mix(in srgb, var(--bg-elevated) 64%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    border-radius: var(--radius-lg);
    padding: 10px 8px;
    min-width: 0;
    box-shadow: var(--shadow-bento);
    transition:
      box-shadow var(--motion-fast) var(--ease),
      background var(--motion-fast) var(--ease);
  }
  /* Drop target: a spectral-tinted glow + accent ring, so the live column reads
     as "release here" without a jarring dashed box. */
  .bcol--over {
    background: color-mix(in srgb, var(--accent-color) 9%, var(--bg-elevated));
    box-shadow:
      var(--shadow-bento),
      inset 0 0 0 1px color-mix(in srgb, var(--accent-color) 55%, transparent),
      var(--glow-accent);
  }
  /* The in-progress column is the board's energetic center — a thin spectral bar
     along its top edge ties it to the list's "doing" thread. Decorative only
     (sits above the glass, never under text); the drop-target ring still wins on
     hover since .bcol--over restyles the whole column. */
  .bcol--doing::before {
    content: "";
    position: absolute;
    top: 0;
    left: 14%;
    right: 14%;
    height: 2px;
    border-radius: 0 0 2px 2px;
    background: var(--grad-spectral);
    opacity: 0.85;
  }
  .bcol--done {
    opacity: 0.72;
  }
  .bcol__head {
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    color: var(--fg-soft);
    margin: 2px 4px 10px;
    display: flex;
    gap: 6px;
    align-items: center;
  }
  .bcol__count {
    color: var(--label-secondary);
    font-family: var(--font-mono);
    font-size: var(--text-footnote);
    font-variant-numeric: tabular-nums;
    background: var(--bg-inset);
    border-radius: 999px;
    padding: 0 6px;
    line-height: 1.7;
  }
  .bcol__cards {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-height: 40px;
  }
  .bcol__empty {
    color: var(--muted);
    font-family: var(--font-mono);
    font-size: var(--text-footnote);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    text-align: center;
    padding: 14px 0;
    border: 0.5px dashed var(--separator-strong);
    border-radius: var(--radius-md);
    margin: 0;
  }
  .bcard {
    background: var(--bg-elevated);
    border: 0.5px solid var(--separator);
    border-left: 3px solid var(--separator-strong);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-card);
    padding: 9px 11px;
    cursor: grab;
    transition:
      background var(--motion-fast) var(--ease),
      border-color var(--motion-fast) var(--ease),
      box-shadow var(--motion-fast) var(--ease);
  }
  /* Hover stays quiet — a faint fill tint + a slightly deeper shadow for a touch
     of lift, no translate (cards don't move under the cursor; the whole card is
     the drag handle). */
  .bcard:hover {
    background: var(--fill-hover);
    border-color: var(--separator-strong);
    box-shadow: var(--shadow-bento);
  }
  .bcard--dragging {
    opacity: 0.4;
  }
  /* Priority color cue, derived from the task's (A..Z) priority — no storage.
     Shares the theme-tuned --pri-* bands with the task rows so A/B/C read
     identically across the list and the board. */
  .bcard.pri-A {
    border-left-color: var(--pri-a);
  }
  .bcard.pri-B {
    border-left-color: var(--pri-b);
  }
  .bcard.pri-C {
    border-left-color: var(--pri-c);
  }
  .bcard__top {
    display: flex;
    gap: 6px;
    align-items: baseline;
  }
  /* Priority letter — the same filled, theme-tuned --pri-* pill the task rows
     use, so A/B/C read identically board-wide (A no longer diverges to plain
     red). Letter + shape carry urgency, never colour alone. */
  .bcard__pri {
    flex: none;
    font-family: var(--font-mono);
    font-size: var(--text-footnote);
    font-weight: 700;
    color: var(--pri-fg);
    background: var(--muted);
    border-radius: var(--radius-xs);
    padding: 0 5px;
    line-height: 1.5;
    box-shadow: inset 0 0.5px 0 rgba(255, 255, 255, 0.35);
  }
  .pri-A .bcard__pri {
    background: var(--pri-a);
  }
  .pri-B .bcard__pri {
    background: var(--pri-b);
  }
  .pri-C .bcard__pri {
    background: var(--pri-c);
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
  /* Card due chip — the due-temperature language, mirroring the list rows. Mono +
     tabular; colour on the card surface (AA both themes) with a hairline ring for
     the active tiers; "later" is a calm bare label. */
  .bcard__recur {
    display: inline-flex;
    align-items: center;
    color: var(--muted);
  }
  /* Estimate chip — a calm, colourless mono pill (mirrors the list row's est) so
     it never competes with the due temperature; sits inline with the tags while
     the deadline keeps the trailing edge. A leading dot stands in for a clock. */
  .bcard__est {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-family: var(--font-mono);
    font-size: var(--text-footnote);
    font-variant-numeric: tabular-nums;
    color: var(--label-secondary);
    background: var(--bg-inset);
    border-radius: 999px;
    padding: 0 6px;
    white-space: nowrap;
  }
  .bcard__est::before {
    content: "";
    width: 5px;
    height: 5px;
    border-radius: 50%;
    border: 1px solid currentColor;
    opacity: 0.7;
  }
  .bcard__due {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    margin-left: auto; /* push the deadline to the trailing edge of the meta row */
    font-family: var(--font-mono);
    font-size: var(--text-footnote);
    font-variant-numeric: tabular-nums;
    color: var(--muted);
    white-space: nowrap;
  }
  .bcard__due--soon {
    color: var(--teal);
    padding: 1px 6px;
    border-radius: 999px;
    box-shadow: inset 0 0 0 0.5px color-mix(in srgb, var(--teal) 45%, transparent);
  }
  .bcard__due--today {
    color: var(--accent-color);
    font-weight: 600;
    padding: 1px 6px;
    border-radius: 999px;
    box-shadow: inset 0 0 0 0.5px color-mix(in srgb, var(--accent-color) 50%, transparent);
  }
  .bcard__due--over {
    color: var(--red);
    font-weight: 600;
    padding: 1px 6px;
    border-radius: 999px;
    box-shadow: inset 0 0 0 0.5px color-mix(in srgb, var(--red) 50%, transparent);
  }
  .bcard__actions {
    display: flex;
    align-items: center;
    gap: 4px;
    justify-content: flex-end;
    margin-top: 6px;
    opacity: 0;
    transition: opacity var(--motion-fast) var(--ease);
  }
  .bcard:hover .bcard__actions {
    opacity: 1;
  }
  /* Keep the actions reachable (and the focus halo visible) for keyboard users,
     who never trigger :hover — reveal whenever anything inside gets focus. */
  .bcard__actions:focus-within {
    opacity: 1;
  }
  /* Quiet keyboard "Move to…" select — sized like the icon buttons, pushed to
     the left of the row so the destructive actions stay at the trailing edge. */
  .bcard__move {
    margin-right: auto;
    max-width: 96px;
    font: inherit;
    font-size: var(--text-footnote);
    padding: 2px 4px;
    color: var(--muted);
    background: var(--fill);
    border: 0.5px solid var(--separator);
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition:
      background var(--motion-fast) var(--ease),
      color var(--motion-fast) var(--ease),
      border-color var(--motion-fast) var(--ease);
  }
  .bcard__move:hover {
    color: var(--fg);
    border-color: var(--separator-strong);
  }
  /* Icon action buttons — local copy of the task-row primitive: a ≥28px macOS
     hit target with fill-based hover/press; the global :focus-visible halo
     (app.css) handles keyboard focus, so no bare outline reset here. */
  .rowbtn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 28px;
    min-height: 28px;
    background: none;
    border: 0.5px solid transparent;
    border-radius: var(--radius-sm);
    color: var(--muted);
    cursor: pointer;
    padding: 0 6px;
    transition:
      background var(--motion-fast) var(--ease),
      color var(--motion-fast) var(--ease),
      transform var(--motion-fast) var(--ease);
  }
  .rowbtn:hover {
    background: var(--fill-hover);
    color: var(--fg);
  }
  .rowbtn:active {
    background: var(--fill-active);
    transform: scale(0.96);
  }
  .rowbtn--danger:hover {
    color: var(--red);
  }
</style>
