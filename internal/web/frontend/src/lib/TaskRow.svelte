<script lang="ts">
  // The one task row, used everywhere a task is shown (Tasks, Review, Today) so a
  // task looks — and behaves — identically regardless of which view surfaced it.
  // It owns its writes: after any change it pushes the fresh groups into the
  // ["tasks"] cache and invalidates the derived task views (review, counts,
  // activity) so every surface stays consistent from a single click.
  import { createMutation, useQueryClient } from "@tanstack/svelte-query";
  import { api, type Task, type TaskGroup } from "./api";
  import { priorityClass, relativeDue, meaningfulSource, displayTitle, fmtDuration } from "./text";
  import { showToast } from "./toast.svelte";
  import { navigate } from "./router.svelte";
  import Icon from "./Icon.svelte";

  let {
    t,
    selected = false,
    onToggleSelect,
  }: {
    t: Task;
    /** True when this row is part of a bulk selection (W11). */
    selected?: boolean;
    /** Toggle this row's bulk selection (x key / the select checkbox). */
    onToggleSelect?: () => void;
  } = $props();

  const qc = useQueryClient();
  const synced = (d: { groups: TaskGroup[] }) => {
    qc.setQueryData(["tasks"], d); // instant in any view subscribed to the task list
    for (const k of [["review"], ["state"], ["activity"], ["tasks-view"]]) {
      qc.invalidateQueries({ queryKey: k }); // tasks-view: recalled saved views re-filter server-side
    }
  };
  // Undo the latest write via the transactional engine; the response is the
  // fresh task groups, so the list snaps back in place. A 409 means the store
  // moved underneath (another writer) — surface that instead of guessing.
  async function undoLast() {
    try {
      synced(await api.undo());
      showToast("Undone");
    } catch (e) {
      showToast(`Couldn't undo: ${String(e)}`);
    }
  }
  const short = () => `“${displayTitle(t.text, 32)}”`;

  const doneMut = createMutation({
    mutationFn: api.taskDone,
    onSuccess: (d) => {
      synced(d);
      showToast(`Completed ${short()}`, undoLast);
    },
  });
  const reopenMut = createMutation({ mutationFn: api.taskReopen, onSuccess: synced });
  const statusMut = createMutation({
    mutationFn: (a: { id: string; status: string }) => api.taskStatus(a.id, a.status),
    onSuccess: synced,
  });
  const deleteMut = createMutation({
    mutationFn: api.taskDelete,
    onSuccess: (d) => {
      synced(d);
      showToast(`Deleted ${short()}`, undoLast);
    },
  });
  const editMut = createMutation({
    mutationFn: (a: { id: string; text: string }) => api.taskEdit(a.id, a.text),
    onSuccess: synced,
  });
  const dueMut = createMutation({
    mutationFn: (a: { id: string; due: string }) => api.taskDue(a.id, a.due),
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
  // Time estimate (todo.txt est:) as a terse "1h 30m" — real data the quick-add
  // preview already speaks, surfaced on the row so a task's weight reads at a
  // glance. 0/absent → no chip (unestimated tasks stay calm). Hidden when done.
  const est = $derived(t.est && t.status !== "done" ? fmtDuration(t.est) : "");

  // Due-date urgency "temperature": a single tier that paints the due chip so the
  // pressure of a deadline reads at a glance — overdue runs hot (red), today is
  // the accent, the next few days are cool teal, anything further out stays muted.
  // Done tasks never carry urgency. Date-only diff (a task due *today* isn't yet
  // overdue), so it agrees with relativeDue()/the agenda buckets — no API change.
  type Urgency = "over" | "today" | "soon" | "later";
  function dueUrgency(dueRaw: string): Urgency {
    const [y, mo, d] = dueRaw.slice(0, 10).split("-").map(Number);
    if (!y || !mo || !d) return "later";
    const now = new Date();
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const diff = Math.round((new Date(y, mo - 1, d).getTime() - today.getTime()) / 86400000);
    if (diff < 0) return "over";
    if (diff === 0) return "today";
    if (diff <= 3) return "soon"; // within the working horizon — worth a cue
    return "later";
  }
  const urgency = $derived(t.due && t.status !== "done" ? dueUrgency(t.due) : null);

  function toggleDoing() {
    $statusMut.mutate({ id: t.id, status: t.status === "doing" ? "open" : "doing" });
  }
  // No confirm() interrogation: delete acts immediately and the toast offers
  // Undo — reversibility beats a blocking dialog (and nt undo still works).
  function del() {
    $deleteMut.mutate(t.id);
  }

  // Give the task a "body": create (or reuse) a linked detail note, then open it
  // in the editor. The note is the task's body — todo.txt itself stays one line.
  let noteBusy = $state(false);
  async function addNote() {
    if (noteBusy) return;
    noteBusy = true;
    try {
      const r = await api.taskNote(t.id);
      synced(await api.tasks()); // refresh so the row shows its new details chip
      navigate(r.url);
    } catch (e) {
      showToast(`Couldn't add a note: ${String(e)}`);
    } finally {
      noteBusy = false;
    }
  }

  // ---- quick reschedule -----------------------------------------------------
  // A tiny preset menu (d on the focused row, or the hover button). Values are
  // the same natural-language tokens quick-add takes; the server's dateparse
  // resolves them, so the menu can never disagree with the store.
  const DUE_PRESETS = [
    { label: "Today", value: "today" },
    { label: "Tomorrow", value: "tomorrow" },
    { label: "Next week", value: "+7d" },
    { label: "No date", value: "none" },
  ];
  let scheduling = $state(false);
  let menuEl: HTMLElement | undefined = $state();
  let schedRestore: HTMLElement | null = null;

  function openSchedule() {
    schedRestore = document.activeElement as HTMLElement;
    scheduling = true;
    queueMicrotask(() => menuEl?.querySelector("button")?.focus());
  }
  function closeSchedule() {
    scheduling = false;
    schedRestore?.focus?.();
    schedRestore = null;
  }
  function pickDue(v: string) {
    $dueMut.mutate({ id: t.id, due: v });
    closeSchedule();
  }
  function onMenuKey(e: KeyboardEvent) {
    e.stopPropagation(); // the row/list handlers must never see menu keys
    if (e.key === "Escape") {
      e.preventDefault();
      closeSchedule();
    } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      const items = [...(menuEl?.querySelectorAll("button") ?? [])];
      const i = items.indexOf(document.activeElement as HTMLButtonElement);
      const n = e.key === "ArrowDown" ? (i + 1) % items.length : (i - 1 + items.length) % items.length;
      items[n]?.focus();
    }
  }

  // Keyboard actions on the focused row (j/k focus is driven by TaskRows). Space
  // or Enter toggles done/reopen; `e` opens the inline editor; `d` opens the
  // reschedule menu. j/k are left to bubble up to the list navigator.
  function onRowKey(e: KeyboardEvent) {
    if (editing || scheduling) return;
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      if (t.status === "done") $reopenMut.mutate(t.id);
      else $doneMut.mutate(t.id);
    } else if (e.key === "e") {
      if (t.status === "done") return;
      e.preventDefault();
      startEdit();
    } else if (e.key === "d") {
      if (t.status === "done") return;
      e.preventDefault();
      openSchedule();
    } else if (e.key === "x") {
      e.preventDefault();
      onToggleSelect?.();
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
  class:row--selected={selected}
  tabindex="-1"
  onkeydown={onRowKey}
>
  {#if onToggleSelect}
    <button
      class="selbox"
      class:selbox--on={selected}
      role="checkbox"
      aria-checked={selected}
      aria-label="Select task"
      title="Select (x)"
      onclick={onToggleSelect}
      ><Icon name={selected ? "square-check" : "square"} filled={selected} size={15} /></button
    >
  {/if}
  {#if pri && t.status !== "done"}
    <span class="pri pri--{pri}" title={`Priority ${t.priority}`} aria-label={`Priority ${t.priority}`}
      >{t.priority}</span
    >
  {/if}
  {#if t.status === "done"}
    <button class="check check--done" title="Reopen" aria-label="Reopen task" onclick={() => $reopenMut.mutate(t.id)}></button>
  {:else}
    <button class="check" title="Mark done" aria-label="Mark task done" onclick={() => $doneMut.mutate(t.id)}></button>
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
      <button class="rowbtn" title="Save (↵)" aria-label="Save" onclick={saveEdit}><Icon name="check" size={15} /></button>
      <button class="rowbtn rowbtn--danger" title="Cancel (esc)" aria-label="Cancel" onclick={() => (editing = false)}><Icon name="close" size={14} /></button>
    </span>
  {:else}
    <span class="row__text" class:done={t.status === "done"} title={t.text}>{t.text}</span>
    {#if t.recur}<span class="row__recur" title="Recurring task"><Icon name="repeat" size={13} /></span>{/if}
    {#if t.status === "doing"}<span class="status-pill status-pill--doing">doing</span>{/if}
    {#if t.status === "blocked"}<span class="status-pill status-pill--blocked" title={t.blocker ? `blocked by: ${t.blocker}` : "blocked"}><Icon name="blocked" size={11} /> blocked</span>{/if}
    {#if t.noteUrl}<a class="chip chip--note" href={t.noteUrl} title={`Details: ${t.noteTitle ?? ""}`}><Icon name="document" size={12} /> details</a>{/if}
    {#if t.project}<a class="chip chip--proj" href={`/search?tag=${encodeURIComponent(t.project)}`}>+{t.project}</a>{/if}
    {#each t.tags ?? [] as tag (tag)}<a class="chip chip--tag" href={`/search?tag=${encodeURIComponent(tag)}`}>@{tag}</a>{/each}
    {#if est}<span class="row__est" title={`Estimated ${est}`} aria-label={`Estimated ${est}`}>{est}</span>{/if}
    {#if due}<span
        class="row__due {urgency ? `row__due--${urgency}` : ''}"
        title={t.due}>{#if urgency === "over"}<Icon name="flame" size={11} filled />{/if}{due.label}</span
      >{/if}
    {#if src}<span class="src src--agent" title={`Captured by ${src}`}>{src}</span>{/if}
    {#if t.status !== "done"}
      <span class="row__actions">
        {#if !t.noteUrl}
          <button class="rowbtn" title="Add a details note (body)" aria-label="Add details note" disabled={noteBusy} onclick={addNote}><Icon name="plus" size={14} /></button>
        {/if}
        <button class="rowbtn" title="Reschedule (d)" aria-label="Reschedule task" onclick={openSchedule}><Icon name="calendar" size={15} /></button>
        <button class="rowbtn" title="Edit text" aria-label="Edit task text" onclick={startEdit}><Icon name="edit" size={15} /></button>
        <button class="rowbtn" aria-label={t.status === "doing" ? "Stop (set open)" : "Start (set doing)"} title={t.status === "doing" ? "Stop (set open)" : "Start (set doing)"} onclick={toggleDoing}><Icon name="doing" size={15} /></button>
        <button class="rowbtn rowbtn--danger" aria-label="Delete task" title="Delete (undoable)" onclick={del}><Icon name="trash" size={15} /></button>
      </span>
    {/if}
    {#if scheduling}
      <div class="sched__backdrop" role="presentation" onclick={closeSchedule}></div>
      <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
      <div
        class="sched"
        role="menu"
        aria-label="Reschedule task"
        tabindex="-1"
        bind:this={menuEl}
        onkeydown={onMenuKey}
      >
        {#each DUE_PRESETS as p (p.value)}
          <button role="menuitem" class="sched__item" onclick={() => pickDue(p.value)}>{p.label}</button>
        {/each}
      </div>
    {/if}
  {/if}
</li>

<style>
  /* Center the check/text/metadata (the global .row uses baseline, for prose
     lists); every task row shares this so Tasks, Review, and Today line up. */
  .row {
    align-items: center;
    gap: 8px;
    position: relative; /* anchors the reschedule popover */
  }
  /* Roving keyboard focus (j/k). Programmatic focus, so we style :focus directly
     rather than :focus-visible (which can skip scripted focus). */
  .row:focus {
    outline: 2px solid var(--accent-color);
    outline-offset: -2px;
    border-radius: var(--radius-sm);
  }
  /* Bulk selection (W11): a calm tinted row + a checkbox that stays hidden until
     hover/selection so unselected rows keep their clean look. */
  .row--selected {
    background: var(--accent-tint);
    border-radius: var(--radius-sm);
  }
  .selbox {
    flex: none;
    background: none;
    border: none;
    color: var(--muted);
    cursor: pointer;
    font-size: 0.95rem;
    line-height: 1;
    padding: 0 2px;
    opacity: 0;
    transition: opacity 0.1s;
  }
  .row:hover .selbox,
  .row:focus .selbox,
  .selbox--on {
    opacity: 1;
  }
  .selbox--on {
    color: var(--accent-color);
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
    border: 1px solid var(--accent-color);
    border-radius: var(--radius-sm);
    color: var(--fg);
  }
  .row__edit:focus {
    outline: none;
    box-shadow: var(--focus-ring-tight);
  }
  /* Hover-revealed inline actions: a borderless ghost button sized to a proper
     macOS hit target (≥28px), with fill-based hover/press and the global focus
     halo (it picks up :focus-visible from app.css). */
  .rowbtn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 28px;
    min-height: 28px;
    background: none;
    border: 1px solid transparent;
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
  .rowbtn:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .rowbtn--danger:hover {
    color: var(--red);
  }
  .status-pill {
    font-family: var(--font-mono);
    font-size: 0.62rem;
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    padding: 1px 7px;
    border-radius: 999px;
  }
  /* A live task's pill — the accent colour (AA in both themes) with a hairline
     ring; the spectral thread lives on the row's decorative left bar/wash, never
     under this text. */
  .status-pill--doing {
    color: var(--accent-color);
    box-shadow: inset 0 0 0 0.5px color-mix(in srgb, var(--accent-color) 50%, transparent);
  }
  .status-pill--blocked {
    color: var(--red);
    box-shadow: inset 0 0 0 0.5px color-mix(in srgb, var(--red) 50%, transparent);
  }
  /* Sharpen the shared .pri primitive locally: a crisper square-ish chip with a
     faint inner light-catch + soft band glow, so priority reads as a deliberate
     marker (shape + letter still carry it, never colour alone). */
  .pri {
    border-radius: var(--radius-xs);
    box-shadow:
      inset 0 0.5px 0 rgba(255, 255, 255, 0.35),
      0 1px 4px -1px color-mix(in srgb, currentColor 0%, transparent);
  }
  .pri--a {
    box-shadow:
      inset 0 0.5px 0 rgba(255, 255, 255, 0.35),
      0 1px 5px -1px color-mix(in srgb, var(--pri-a) 55%, transparent);
  }
  .pri--b {
    box-shadow:
      inset 0 0.5px 0 rgba(255, 255, 255, 0.35),
      0 1px 5px -1px color-mix(in srgb, var(--pri-b) 50%, transparent);
  }
  /* Project chip: a quiet fill with a subtle accent edge so a task's home reads
     as a structural anchor, distinct from the @tag chips. */
  .chip--proj {
    color: var(--accent-color);
    box-shadow: inset 0 0 0 0.5px color-mix(in srgb, var(--accent-color) 40%, transparent);
  }
  .chip--proj:hover {
    background: var(--accent-tint);
    text-decoration: none;
  }
  .chip--tag {
    color: var(--accent-2);
  }
  .chip--tag:hover {
    background: color-mix(in srgb, var(--accent-2) 12%, transparent);
    text-decoration: none;
  }
  /* The "details" chip linking to a task's body note. */
  .chip--note {
    color: var(--accent-color);
    background: var(--accent-tint);
  }
  .chip--note:hover {
    text-decoration: underline;
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
  /* The "doing" accent wins over the priority bar when both apply — a live task
     gets the signature spectral thread + a faint wash, so in-progress work glows
     a touch warmer than the rest of the list without breaking its calm. */
  .row--doing {
    box-shadow: inset 3px 0 0 0 var(--spectral-2);
    background: linear-gradient(
      90deg,
      color-mix(in srgb, var(--spectral-2) 7%, transparent),
      transparent 28%
    );
    border-radius: var(--radius-sm);
  }
  /* Due-date temperature: one label whose colour encodes how much pressure the
     deadline carries — overdue runs hot (red + flame), today is the accent, the
     next few days are cool teal, anything further out stays a calm muted label.
     The colour sits on the plain content surface (not a coloured fill) so every
     tier clears WCAG AA in both themes; a hairline ring gives today/soon/over
     their pill shape without eroding contrast. Mono + tabular from .row__due. */
  .row__due {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  /* Time-estimate chip (todo.txt est:) — a calm, neutral mono pill carrying the
     task's weight. Deliberately colourless so it never competes with the due
     temperature; a leading dot stands in for a clock glyph at this size. */
  .row__est {
    flex: none; /* metadata keeps intrinsic size; only .row__text (flex:1) truncates */
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-family: var(--font-mono);
    font-size: var(--text-footnote);
    font-variant-numeric: tabular-nums;
    color: var(--label-secondary);
    background: var(--bg-inset);
    border-radius: 999px;
    padding: 1px 7px;
    white-space: nowrap;
  }
  .row__est::before {
    content: "";
    width: 5px;
    height: 5px;
    border-radius: 50%;
    border: 1px solid currentColor;
    opacity: 0.7;
  }
  .row__due--later {
    color: var(--muted);
  }
  .row__due--soon {
    color: var(--teal);
    padding: 1px 7px;
    border-radius: 999px;
    box-shadow: inset 0 0 0 0.5px color-mix(in srgb, var(--teal) 45%, transparent);
  }
  .row__due--today {
    color: var(--accent-color);
    font-weight: 600;
    padding: 1px 7px;
    border-radius: 999px;
    box-shadow: inset 0 0 0 0.5px color-mix(in srgb, var(--accent-color) 50%, transparent);
  }
  .row__due--over {
    color: var(--red);
    font-weight: 600;
    padding: 1px 7px;
    border-radius: 999px;
    box-shadow: inset 0 0 0 0.5px color-mix(in srgb, var(--red) 50%, transparent);
  }
  /* Quick-reschedule popover: a small anchored menu of due presets. The fixed
     transparent backdrop makes any outside click dismiss it. */
  .sched__backdrop {
    position: fixed;
    inset: 0;
    z-index: 19;
  }
  .sched {
    position: absolute;
    right: 8px;
    top: calc(100% - 2px);
    z-index: 20;
    display: flex;
    flex-direction: column;
    min-width: 132px;
    padding: 4px;
    background: color-mix(in srgb, var(--bg-elevated) 90%, transparent);
    -webkit-backdrop-filter: blur(20px) saturate(170%);
    backdrop-filter: blur(20px) saturate(170%);
    border: 0.5px solid var(--separator);
    border-radius: var(--radius-popover);
    box-shadow: var(--shadow-popover);
    transform-origin: top right;
    animation: popover-in var(--motion) var(--ease-spring);
  }
  .sched:focus {
    outline: none;
  }
  .sched__item {
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
  /* Arrow-key focus is programmatic (:focus, not :focus-visible), so highlight
     the active item directly; mouse hover gets the same calm fill. */
  .sched__item:hover,
  .sched__item:focus {
    background: var(--fill-hover);
    outline: none;
  }
  .sched__item:focus-visible {
    background: var(--accent-fill);
    color: var(--on-accent);
  }

  /* Agent-captured tasks (e.g. src:claude) — nt's reason to exist; quiet, but
     legible, so you can tell what your AI session added. */
  .src--agent {
    color: var(--accent-color);
    background: var(--accent-tint);
    padding: 0 5px;
    border-radius: 999px;
    font-size: 0.7rem;
  }
</style>
