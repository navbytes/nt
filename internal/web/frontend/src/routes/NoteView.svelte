<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import { noteUI } from "../lib/noteUI.svelte";
  import { navigate } from "../lib/router.svelte";
  import Editor from "../lib/Editor.svelte";
  import Icon from "../lib/Icon.svelte";
  import MindMap from "../lib/MindMap.svelte";
  import { parseOutline } from "../lib/outline";
  import { mapSearch } from "../lib/mindmap";
  import { wikiTree } from "../lib/wikimap";
  import { renderMermaidIn, observeTheme } from "../lib/mermaid";
  import { tick } from "svelte";

  let { handle }: { handle: string } = $props();

  const qc = useQueryClient();
  const noteQ = createQuery({ queryKey: ["note", handle], queryFn: () => api.note(handle) });
  const notesQ = createQuery({ queryKey: ["notes"], queryFn: api.notes });

  let editing = $state(false);
  // The map view + source are seeded from the URL (?view=map[&map=links]) so a
  // mind map is a shareable, reload-surviving deep link. NoteView is keyed on the
  // handle alone, so it remounts per note — reading the query at init is enough;
  // an effect below writes changes back to the address bar.
  let mapView = $state(new URLSearchParams(location.search).get("view") === "map");
  let mapSource = $state<"outline" | "links">(
    new URLSearchParams(location.search).get("map") === "links" ? "links" : "outline",
  );
  let activeId = $state("");
  // Ref to the rendered note body, so Mermaid renders into THIS note's prose and
  // not the editor preview's also-".prose" pane (W33).
  let bodyEl: HTMLElement | undefined = $state();

  // ---- move to folder (the note keeps its id/URL; links are rewritten) ----
  let moving = $state(false);
  let moveTarget = $state("");
  let moveErr = $state("");
  let moveBusy = $state(false);
  let moveInput: HTMLInputElement | undefined = $state();

  // Existing folders, for the picker's autocomplete (you can also type a new one).
  const folders = $derived.by(() => {
    const set = new Set<string>();
    for (const link of $notesQ.data?.index ?? []) {
      const i = link.path.lastIndexOf("/");
      if (i > 0) set.add(link.path.slice(0, i));
    }
    return [...set].sort();
  });

  function openMove() {
    moveErr = "";
    moveTarget = $noteQ.data?.folder ?? "";
    moving = true;
    // Focus lands in the fresh bar (it may be opened from the command palette,
    // which restores focus to its opener on close).
    queueMicrotask(() => moveInput?.focus());
  }

  // Open the move picker when the command palette requests it.
  let seenMoveReq = noteUI.moveRequest;
  $effect(() => {
    if (noteUI.moveRequest !== seenMoveReq) {
      seenMoveReq = noteUI.moveRequest;
      openMove();
    }
  });
  async function doMove() {
    moveErr = "";
    moveBusy = true;
    try {
      await api.noteMove(handle, moveTarget.trim());
      await qc.invalidateQueries({ queryKey: ["notes"] });
      await qc.invalidateQueries({ queryKey: ["note", handle] });
      moving = false;
    } catch (e) {
      moveErr = String(e);
    } finally {
      moveBusy = false;
    }
  }

  // ---- archive / unarchive (a soft, reversible retire) ----
  let archiveBusy = $state(false);
  async function doArchive() {
    const want = !($noteQ.data?.archived ?? false);
    archiveBusy = true;
    try {
      await api.noteArchive(handle, want);
      // Refresh the surfaces the flag touches: this note (button label), the
      // sidebar/grid (it appears/disappears), and the orphan/graph/state counts.
      for (const k of [["note", handle], ["notes"], ["notesGrid"], ["orphans"], ["graph"], ["state"]]) {
        await qc.invalidateQueries({ queryKey: k });
      }
    } finally {
      archiveBusy = false;
    }
  }

  // ---- favorite / unfavorite (a lightweight star, orthogonal to archive) ----
  let favBusy = $state(false);
  async function doFavorite() {
    const want = !($noteQ.data?.favorite ?? false);
    favBusy = true;
    try {
      await api.noteFavorite(handle, want);
      // The star shows here and on the grid (where the Favorites filter lives).
      for (const k of [["note", handle], ["notesGrid"]]) {
        await qc.invalidateQueries({ queryKey: k });
      }
    } finally {
      favBusy = false;
    }
  }

  // ---- delete (to .trash) with backlink handling, mirroring the CLI/TUI ----
  let confirmingDelete = $state(false);
  let deleteBusy = $state(false);
  let deleteErr = $state("");
  let deleteCancelBtn: HTMLButtonElement | undefined = $state();

  function openDelete() {
    deleteErr = "";
    confirmingDelete = true;
    // Focus the safe action, not a destructive default.
    queueMicrotask(() => deleteCancelBtn?.focus());
  }

  async function doDelete(mode: "" | "unlink" | "force") {
    deleteErr = "";
    deleteBusy = true;
    try {
      await api.noteDelete(handle, mode);
      // The note is gone from every surface — refresh them and leave the page.
      for (const k of [["notes"], ["notesGrid"], ["orphans"], ["graph"], ["state"], ["tags"]]) {
        await qc.invalidateQueries({ queryKey: k });
      }
      confirmingDelete = false;
      navigate("/notes");
    } catch (e) {
      deleteErr = String(e);
    } finally {
      deleteBusy = false;
    }
  }

  // ---- new task linked to this note (closes the note→task loop) ----
  let addingTask = $state(false);
  let taskText = $state("");
  let taskErr = $state("");
  let taskBusy = $state(false);
  let taskInput: HTMLInputElement | undefined = $state();

  function toggleAddTask() {
    addingTask = !addingTask;
    if (addingTask) queueMicrotask(() => taskInput?.focus());
  }

  async function doAddTask() {
    const body = taskText.trim();
    if (!body) return;
    taskErr = "";
    taskBusy = true;
    try {
      // Append a [[wikilink]] to the note title so the task references this note
      // (it then shows under "Referenced by tasks").
      await api.taskNew(`${body} [[${$noteQ.data?.title ?? handle}]]`);
      await qc.invalidateQueries({ queryKey: ["note", handle] });
      await qc.invalidateQueries({ queryKey: ["tasks"] });
      await qc.invalidateQueries({ queryKey: ["state"] });
      taskText = "";
      addingTask = false;
    } catch (e) {
      taskErr = String(e);
    } finally {
      taskBusy = false;
    }
  }

  interface TocItem {
    id: string;
    text: string;
    level: number;
  }

  // Build the "On this page" outline from the server-rendered body (goldmark
  // gives every heading a stable id), so it always matches the rendered HTML.
  function extractToc(html: string): TocItem[] {
    if (!html || typeof DOMParser === "undefined") return [];
    const doc = new DOMParser().parseFromString(html, "text/html");
    const items: TocItem[] = [];
    doc.querySelectorAll("h1, h2, h3").forEach((h) => {
      if (h.id) items.push({ id: h.id, text: h.textContent ?? "", level: Number(h.tagName[1]) });
    });
    return items;
  }

  const toc = $derived(extractToc($noteQ.data?.bodyHTML ?? ""));

  // The mind map is the note's own outline (headings + nested lists) as a radial
  // tree, parsed from the same server-rendered HTML the prose shows — so its
  // heading nodes carry goldmark's ids and can scroll the reader to that section.
  const outline = $derived(parseOutline($noteQ.data?.bodyHTML ?? "", $noteQ.data?.title ?? ""));
  const hasOutline = $derived(outline.root.children.length > 0);

  // The "Links" source roots on this note and radiates along wikilinks. The graph
  // payload is fetched lazily — only while the links map is on — and shares the
  // /graph route's cache. Wrapping createQuery in $derived makes `enabled` react.
  const wantLinks = $derived(mapView && mapSource === "links");
  const graphQ = $derived(
    createQuery({ queryKey: ["graph"], queryFn: api.graph, enabled: wantLinks }),
  );
  const linkTree = $derived.by(() =>
    $graphQ.data ? wikiTree($graphQ.data, handle, 3) : null,
  );
  // The tree MindMap renders: internal outline, or the wikilink tree once loaded.
  const mapTree = $derived(mapSource === "links" ? linkTree : outline);

  // Mirror the map state into the URL so it survives reload and is shareable. We
  // edit the real address bar (not the router) so it never remounts this view;
  // mapSearch preserves any other params and we only write on an actual change.
  $effect(() => {
    const next = mapSearch(location.search, mapView, mapSource);
    if (next !== location.search.replace(/^\?/, "")) {
      history.replaceState(history.state, "", location.pathname + (next ? `?${next}` : ""));
    }
  });

  // Activating a map node: outline headings scroll the prose (anchor is a heading
  // id); wikilink nodes navigate to that note (anchor is a "/n/…" url).
  async function onMapJump(anchor: string) {
    if (anchor.startsWith("/")) {
      navigate(anchor);
      return;
    }
    mapView = false;
    await tick();
    document.getElementById(anchor)?.scrollIntoView({ behavior: "smooth", block: "start" });
    history.replaceState(null, "", "#" + anchor);
    activeId = anchor;
  }

  // Entering the map: prefer the outline, but fall back to links for a note with
  // no internal structure (e.g. a pure hub note).
  function openMap() {
    mapSource = hasOutline ? "outline" : "links";
    mapView = true;
  }

  // Scroll-spy: highlight the heading nearest the top of the viewport.
  $effect(() => {
    const items = toc;
    if (!items.length || typeof IntersectionObserver === "undefined") return;
    const headings = items
      .map((i) => document.getElementById(i.id))
      .filter((el): el is HTMLElement => el !== null);
    if (!headings.length) return;
    const obs = new IntersectionObserver(
      (entries) => {
        for (const e of entries) if (e.isIntersecting) activeId = (e.target as HTMLElement).id;
      },
      { rootMargin: "-64px 0px -70% 0px" },
    );
    headings.forEach((h) => obs.observe(h));
    return () => obs.disconnect();
  });

  function jump(e: MouseEvent, id: string) {
    e.preventDefault();
    document.getElementById(id)?.scrollIntoView({ behavior: "smooth", block: "start" });
    history.replaceState(null, "", "#" + id);
    activeId = id;
  }

  // Run Mermaid on the rendered note body — after mount / on body change, and
  // again on theme toggle so diagrams re-render in the matching light/dark
  // theme (shared with the editor's live preview via lib/mermaid).
  $effect(() => {
    const html = $noteQ.data?.bodyHTML;
    void mapView; // re-run when returning from the mind map so diagrams re-render
    if (!html || mapView) return;
    let cancelled = false;
    const render = () => {
      // Scope to THIS note's body via a ref — the global ".prose" selector also
      // matches the editor's live-preview pane, so the first match could be the
      // wrong element (W33).
      const el = bodyEl;
      if (el && !cancelled) void renderMermaidIn(el);
    };
    const id = setTimeout(render, 0); // defer so the DOM is updated first
    const disconnect = observeTheme(render);
    return () => {
      cancelled = true;
      clearTimeout(id);
      disconnect();
    };
  });
</script>

{#if editing}
  <Editor {handle} onClose={() => (editing = false)} />
{:else if $noteQ.isPending}
  <p class="muted">Loading…</p>
{:else if $noteQ.error}
  <div class="empty empty--hero">
    <span class="empty__art empty__art--err"><Icon name="warning" size={24} /></span>
    <p class="empty__lead">Note not found.</p>
    <p class="muted">This note may have been moved, renamed, or deleted.</p>
    <a class="btn btn--ghost btn--sm" href="/notes">Back to notes</a>
  </div>
{:else if $noteQ.data}
  {@const n = $noteQ.data}
  <div class="notewrap">
    <article class="note">
      <div class="crumbs">
        <a class="crumbs__root" href="/notes">Notes</a>
        {#each n.crumbs ?? [] as c (c)}<span>{c}</span>{/each}
        <span class="crumbs__file">{n.file}</span>
        <span class="spacer"></span>
        <div class="pillbar__row">
          <div class="pillbar">
            <a class="pillbar__btn" href={`/graph?focus=${encodeURIComponent(handle)}`} title="Graph"><Icon name="focus" size={15} /> <span class="pillbar__btn-label">Graph</span></a>
            <span class="pillbar__sep"></span>
            <button
              class="pillbar__btn"
              class:pillbar__btn--on={mapView}
              onclick={() => (mapView ? (mapView = false) : openMap())}
              aria-pressed={mapView}
              title={mapView ? "Show the note" : "Mind map of this note"}
            ><Icon name="graph" size={15} /> <span class="pillbar__btn-label">{mapView ? "Note" : "Mind map"}</span></button>
            <span class="pillbar__sep"></span>
            <button
              class="pillbar__btn pillbar__btn--icon"
              class:pillbar__btn--on={n.favorite}
              onclick={doFavorite}
              disabled={favBusy}
              aria-pressed={n.favorite}
              aria-label={n.favorite ? "Remove from favorites" : "Add to favorites"}
              title={n.favorite ? "Remove from favorites" : "Add to favorites"}
            ><Icon name="star" filled={n.favorite} size={16} /></button>
          </div>
          <div class="pillbar">
            <button class="pillbar__btn" onclick={toggleAddTask} title="Task"><Icon name="plus" size={15} /> <span class="pillbar__btn-label">Task</span></button>
            <span class="pillbar__sep"></span>
            <button class="pillbar__btn" onclick={openMove} title="Move"><Icon name="folder" size={15} /> <span class="pillbar__btn-label">Move</span></button>
            <span class="pillbar__sep"></span>
            <button class="pillbar__btn" onclick={doArchive} disabled={archiveBusy} title={n.archived ? "Unarchive" : "Archive"}>
              <Icon name="archive" size={15} /> <span class="pillbar__btn-label">{n.archived ? "Unarchive" : "Archive"}</span>
            </button>
            <span class="pillbar__sep"></span>
            <button class="pillbar__btn" onclick={() => (editing = true)} title="Edit"><Icon name="edit" size={15} /> <span class="pillbar__btn-label">Edit</span></button>
          </div>
          <div class="pillbar">
            <button class="pillbar__btn pillbar__btn--danger" onclick={openDelete} title="Delete"><Icon name="trash" size={15} /> <span class="pillbar__btn-label">Delete</span></button>
          </div>
        </div>
      </div>
      {#if confirmingDelete}
        <div class="movebar movebar--danger">
          {#if n.backlinks?.length}
            <span class="movebar__label">
              Delete “{n.title}”? {n.backlinks.length} note{n.backlinks.length === 1 ? "" : "s"} link here — see “Linked from” below.
            </span>
            <button class="btn btn--sm" onclick={() => doDelete("unlink")} disabled={deleteBusy}>Unlink &amp; delete</button>
            <button class="btn btn--sm btn--danger" onclick={() => doDelete("force")} disabled={deleteBusy}>Delete anyway</button>
          {:else}
            <span class="movebar__label">Delete “{n.title}”? Moves it to .trash/.</span>
            <button class="btn btn--sm btn--danger" onclick={() => doDelete("")} disabled={deleteBusy}>Delete</button>
          {/if}
          <button class="btn btn--ghost btn--sm" bind:this={deleteCancelBtn} onclick={() => (confirmingDelete = false)}>Cancel</button>
          {#if deleteErr}<span class="error small" role="alert">{deleteErr}</span>{/if}
        </div>
      {/if}
      {#if addingTask}
        <div class="movebar">
          <span class="movebar__label">New task</span>
          <input
            class="movebar__input"
            bind:this={taskInput}
            bind:value={taskText}
            placeholder="what needs doing? (linked to this note)"
            onkeydown={(e) => e.key === "Enter" && doAddTask()}
          />
          <button class="btn btn--sm" onclick={doAddTask} disabled={taskBusy}>{taskBusy ? "Adding…" : "Add task"}</button>
          <button class="btn btn--ghost btn--sm" onclick={() => (addingTask = false)}>Cancel</button>
          {#if taskErr}<span class="error small" role="alert">{taskErr}</span>{/if}
        </div>
      {/if}
      {#if moving}
        <div class="movebar">
          <span class="movebar__label">Move to folder</span>
          <input
            class="movebar__input"
            list="note-folders"
            bind:this={moveInput}
            bind:value={moveTarget}
            placeholder="(root)"
            onkeydown={(e) => e.key === "Enter" && doMove()}
          />
          <datalist id="note-folders">
            {#each folders as f (f)}<option value={f}></option>{/each}
          </datalist>
          <button class="btn btn--sm" onclick={doMove} disabled={moveBusy}>{moveBusy ? "Moving…" : "Move"}</button>
          <button class="btn btn--ghost btn--sm" onclick={() => (moving = false)}>Cancel</button>
          {#if moveErr}<span class="error small" role="alert">{moveErr}</span>{/if}
        </div>
      {/if}
      {#if n.archived}
        <div class="archived-banner" role="note">
          <Icon name="archive" size={15} />
          <span>Archived — hidden from the sidebar, search, orphans, and the graph. Still on disk.</span>
        </div>
      {/if}
      <h1 class="note__title">{n.title}</h1>
      <div class="note__meta">
        {#if n.folder}<span class="note__metaitem note__folder"><Icon name="document" size={13} /> {n.folder}</span>{/if}
        {#if n.created}<span class="note__metaitem note__date"><Icon name="calendar" size={13} /> {n.created}</span>{/if}
        {#if n.backlinks?.length}<a class="note__metaitem note__links" href="#linked-from"><Icon name="graph" size={13} /> {n.backlinks.length} backlink{n.backlinks.length === 1 ? "" : "s"}</a>{/if}
        {#if n.source}<span class="note__metaitem src">{n.source}</span>{/if}
        {#if n.tags?.length}
          <span class="note__tags">
            {#each n.tags as t (t)}<a class="chip" href={`/search?tag=${encodeURIComponent(t)}`}>#{t}</a>{/each}
          </span>
        {/if}
      </div>

      {#if mapView}
        <div class="mapbar" role="group" aria-label="Mind map source">
          <div class="seg">
            <button
              class:seg--on={mapSource === "outline"}
              aria-pressed={mapSource === "outline"}
              disabled={!hasOutline}
              onclick={() => (mapSource = "outline")}
              title={hasOutline ? "Map this note's headings & lists" : "This note has no headings or lists to map"}
            >Outline</button>
            <button
              class:seg--on={mapSource === "links"}
              aria-pressed={mapSource === "links"}
              onclick={() => (mapSource = "links")}
              title="Map notes linked from here via [[wikilinks]]"
            >Links</button>
          </div>
        </div>
        {#if mapSource === "links" && $graphQ.isPending}
          <div class="map-msg muted">Loading the link graph…</div>
        {:else if mapTree && mapTree.root.children.length}
          {#key mapSource}
            <MindMap root={mapTree.root} truncated={mapTree.truncated} onJump={onMapJump} />
          {/key}
        {:else if mapSource === "links"}
          <div class="map-msg muted">No linked notes yet — add <code>[[wikilinks]]</code> to build a web.</div>
        {:else}
          <div class="map-msg muted">Nothing to map — this note has no headings or lists.</div>
        {/if}
      {:else}
        <!-- bodyHTML is rendered server-side by goldmark (safe mode, escaped). -->
        <div class="prose" bind:this={bodyEl}>{@html n.bodyHTML}</div>
      {/if}

      {#if n.taskRefs.length}
        <section class="panel">
          <h2 class="group__title">Referenced by tasks</h2>
          <ul class="rows">
            {#each n.taskRefs as ref (ref.text)}
              <li class="row"><span class="row__text">{ref.text}</span><span class="src">{ref.source}</span></li>
            {/each}
          </ul>
        </section>
      {/if}

      {#if n.backlinks?.length}
        <section class="panel" id="linked-from">
          <h2 class="group__title">Linked from</h2>
          <ul class="rows">
            {#each n.backlinks as bl (bl.url + bl.text)}
              <li class="row">
                {#if bl.isNote}<a href={bl.url}>{bl.title}</a>{:else}<span class="row__text">{bl.text}</span>{/if}
              </li>
            {/each}
          </ul>
        </section>
      {/if}

      {#if n.prev || n.next}
        <nav class="prevnext" aria-label="Adjacent notes">
          {#if n.prev}
            <a class="prevnext__link prevnext__link--prev" href={n.prev.url}>
              <Icon name="chevron-left" size={15} />
              <span class="prevnext__col">
                <span class="prevnext__dir">Previous</span>
                <span class="prevnext__title">{n.prev.title}</span>
              </span>
            </a>
          {/if}
          <span class="spacer"></span>
          {#if n.next}
            <a class="prevnext__link prevnext__link--next" href={n.next.url}>
              <span class="prevnext__col">
                <span class="prevnext__dir">Next</span>
                <span class="prevnext__title">{n.next.title}</span>
              </span>
              <Icon name="chevron-right" size={15} />
            </a>
          {/if}
        </nav>
      {/if}
    </article>

    {#if toc.length > 1}
      <nav class="toc" aria-label="On this page">
        <div class="toc__head">On this page</div>
        <div class="toc__links">
          {#each toc as item (item.id)}
            <a
              href={"#" + item.id}
              class="toc__link toc__link--l{item.level}"
              class:active={activeId === item.id}
              onclick={(e) => jump(e, item.id)}>{item.text}</a
            >
          {/each}
        </div>
      </nav>
    {/if}
  </div>
{/if}

<style>
  /* ── Mind-map source bar ─────────────────────────────────────────────────
     A small segmented control above the map to pick outline vs wikilink source
     (mirrors the graph cockpit's segmented control, kept local to this view). */
  .mapbar {
    display: flex;
    margin: var(--space-3, 10px) 0;
  }
  .mapbar .seg {
    display: inline-flex;
    gap: 2px;
    padding: 2px;
    background: var(--fill);
    border: 0.5px solid var(--separator);
    border-radius: var(--radius-sm, 6px);
  }
  .mapbar .seg button {
    padding: 4px 12px;
    background: transparent;
    border: 0.5px solid transparent;
    border-radius: var(--radius-xs, 4px);
    color: var(--fg-soft);
    cursor: pointer;
    font-size: var(--text-callout, 12px);
  }
  .mapbar .seg button:hover:not(.seg--on):not(:disabled) {
    color: var(--fg);
  }
  .mapbar .seg button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .mapbar .seg--on {
    background: color-mix(in srgb, var(--spectral-2) 40%, var(--bg-elevated));
    border-color: color-mix(in srgb, var(--spectral-2) 60%, transparent);
    color: var(--fg);
    font-weight: 600;
  }
  .map-msg {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 200px;
    text-align: center;
    border: 0.5px dashed var(--separator);
    border-radius: var(--radius-lg, 12px);
    padding: var(--space-4, 16px);
  }
  .map-msg code {
    font-family: var(--font-mono, monospace);
    font-size: 0.9em;
    padding: 1px 4px;
    background: var(--fill);
    border-radius: 4px;
  }

  /* The action bar (crumbs + tools) is chrome above the article: a refined mono
     breadcrumb trail, and the note tools grouped into frosted-glass .pillbar
     clusters (styled globally in app.css). */
  /* Destructive actions read in a muted red and warm fully on hover, so Delete
     is visually distinct from the neutral ghost buttons next to it. */
  .btn--danger {
    color: var(--red);
  }
  .btn--danger:hover {
    background: var(--red);
    color: var(--on-accent);
  }
  .movebar--danger {
    border-color: var(--red);
  }

  /* ── Breadcrumbs ─────────────────────────────────────────────────────────
     Refined trail: a "Notes" home link + folder crumbs in mono, the file name
     emphasized. Overrides the app.css base via component-scoped specificity. */
  .crumbs {
    align-items: center;
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    letter-spacing: var(--tracking-caps);
    text-transform: uppercase;
    color: var(--muted);
    padding-bottom: var(--space-2);
  }
  .crumbs__root {
    color: var(--muted);
    transition: color var(--motion-fast) var(--ease);
  }
  .crumbs__root:hover {
    color: var(--accent-color);
    text-decoration: none;
  }
  .crumbs__file {
    color: var(--label-secondary);
    text-transform: none;
  }

  /* ── Title + metadata ────────────────────────────────────────────────────
     A serif display title (the global h1 is already serif; we scale it up and
     lean into the display tracking), over a mono metadata row. */
  .note__title {
    font-size: var(--text-display-sm);
    letter-spacing: var(--tracking-display);
    margin-bottom: var(--space-3);
  }
  .note__meta {
    gap: var(--space-4);
    margin-bottom: var(--space-7);
    padding-bottom: var(--space-5);
    border-bottom: 0.5px solid var(--separator);
  }
  .note__metaitem {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    letter-spacing: var(--tracking-caps);
    text-transform: uppercase;
    color: var(--muted);
  }
  .note__metaitem :global(.icon) {
    color: var(--label-quaternary);
  }
  .note__folder {
    text-transform: none;
  }
  a.note__links {
    color: var(--muted);
    transition: color var(--motion-fast) var(--ease);
  }
  a.note__links:hover {
    color: var(--accent-color);
    text-decoration: none;
  }
  a.note__links:hover :global(.icon) {
    color: var(--spectral-2);
  }
  .note__tags {
    display: inline-flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }
  /* Tag chips here are links (global .chip is a borderless tint pill); give them
     a faint hairline so they read as affordances in the metadata row. */
  .note__tags .chip {
    color: var(--accent-color);
    background: var(--accent-tint);
    box-shadow: inset 0 0 0 0.5px color-mix(in srgb, var(--accent-color) 22%, transparent);
    transition:
      background var(--motion-fast) var(--ease),
      box-shadow var(--motion-fast) var(--ease);
  }
  .note__tags .chip:hover {
    background: var(--accent-tint-strong);
    text-decoration: none;
  }

  /* ── Archived banner ─────────────────────────────────────────────────────
     A glassy, dashed amber note (kept reversible-feeling). Overrides the
     app.css base with the spectral/teal accent edge. */
  .archived-banner {
    align-items: center;
    background: color-mix(in srgb, var(--teal) 8%, var(--bg-elevated));
    border: 0.5px dashed color-mix(in srgb, var(--teal) 45%, transparent);
    border-left: 3px solid var(--teal);
    border-radius: var(--radius-md);
    color: var(--label-secondary);
  }
  .archived-banner :global(.icon) {
    color: var(--teal);
  }

  /* ── Action / move / task bars ───────────────────────────────────────────
     Glassy inset bars (the app.css base is a flat inset fill). */
  .movebar {
    background: color-mix(in srgb, var(--bg-elevated) 72%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    border: 0.5px solid var(--separator);
    box-shadow: var(--glass-hairline);
    border-radius: var(--radius-md);
  }
  .movebar__label {
    font-family: var(--font-mono);
    letter-spacing: var(--tracking-caps);
  }

  /* ── Related panels (task refs + backlinks) ──────────────────────────────
     The shared .panel/.group__title/.rows globals already carry the mono
     heading + hairline rows; only a touch of polish on the heading + link rows. */
  .panel {
    scroll-margin-top: 72px;
  }
  .panel .group__title {
    font-size: var(--text-callout);
    letter-spacing: var(--tracking-caps);
  }

  /* ── Prev / next ─────────────────────────────────────────────────────────
     Two glass "cards" pulling left/right, each with a mono direction label over
     the (serif-ish) title. Overrides the app.css .prevnext base layout. */
  .prevnext {
    gap: var(--space-4);
    border-top: 0.5px solid var(--separator);
  }
  .prevnext__link {
    flex: 1 1 0;
    display: flex;
    align-items: center;
    gap: var(--space-3);
    max-width: 48%;
    padding: var(--space-4);
    border-radius: var(--radius-md);
    background: color-mix(in srgb, var(--bg-elevated) 70%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    box-shadow: var(--glass-hairline), 0 0 0 0.5px var(--separator);
    color: var(--label-secondary);
    transition:
      box-shadow var(--motion-fast) var(--ease),
      transform var(--motion-fast) var(--ease),
      color var(--motion-fast) var(--ease);
  }
  .prevnext__link--next {
    text-align: right;
    justify-content: flex-end;
  }
  /* mirror the Previous card: right-align the label + title in the Next card */
  .prevnext__link--next .prevnext__col {
    align-items: flex-end;
  }
  .prevnext__link:hover {
    color: var(--label-primary);
    text-decoration: none;
    transform: translateY(-1px);
    box-shadow: var(--shadow-bento);
  }
  .prevnext__link :global(.icon) {
    flex: none;
    color: var(--muted);
    transition: color var(--motion-fast) var(--ease);
  }
  .prevnext__link:hover :global(.icon) {
    color: var(--spectral-2);
  }
  .prevnext__col {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .prevnext__link--next .prevnext__col {
    align-items: flex-end;
  }
  .prevnext__dir {
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    color: var(--muted);
  }
  .prevnext__title {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 100%;
  }

  /* ── Table of contents ───────────────────────────────────────────────────
     The app.css base draws the rail + active state; here we lift the active
     link to a spectral accent (text + a spectral left border) and add a mono
     header treatment, matching the sidebar/nav microlabel language. */
  .toc__head {
    letter-spacing: var(--tracking-caps);
    padding-left: 10px;
  }
  .toc__link {
    transition:
      color var(--motion-fast) var(--ease),
      border-color var(--motion-fast) var(--ease),
      background var(--motion-fast) var(--ease);
  }
  .toc__link.active {
    /* The active label needs AA on the content surface (spectral-2 was ~3.99:1);
       the spectral hue stays as the left-border accent. */
    color: var(--accent-on-tint, #0a52b8);
    border-left-color: var(--spectral-2);
    font-weight: 600;
  }

  @media (max-width: 640px) {
    .prevnext__link {
      max-width: none;
    }
  }
</style>
