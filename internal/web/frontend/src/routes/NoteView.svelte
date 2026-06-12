<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import { noteUI } from "../lib/noteUI.svelte";
  import Editor from "../lib/Editor.svelte";
  import { renderMermaidIn, observeTheme } from "../lib/mermaid";

  let { handle, canEdit = false }: { handle: string; canEdit?: boolean } = $props();

  const qc = useQueryClient();
  const noteQ = createQuery({ queryKey: ["note", handle], queryFn: () => api.note(handle) });
  const notesQ = createQuery({ queryKey: ["notes"], queryFn: api.notes });

  let editing = $state(false);
  let activeId = $state("");

  // ---- move to folder (the note keeps its id/URL; links are rewritten) ----
  let moving = $state(false);
  let moveTarget = $state("");
  let moveErr = $state("");
  let moveBusy = $state(false);

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
  }

  // Open the move picker when the command palette requests it (canEdit only).
  let seenMoveReq = noteUI.moveRequest;
  $effect(() => {
    if (canEdit && noteUI.moveRequest !== seenMoveReq) {
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

  // ---- new task linked to this note (closes the note→task loop) ----
  let addingTask = $state(false);
  let taskText = $state("");
  let taskErr = $state("");
  let taskBusy = $state(false);

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
    if (!html) return;
    let cancelled = false;
    const render = () => {
      const el = document.querySelector<HTMLElement>(".prose");
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
  <p class="error">Note not found.</p>
{:else if $noteQ.data}
  {@const n = $noteQ.data}
  <div class="notewrap">
    <article class="note">
      <div class="crumbs">
        {#each n.crumbs as c (c)}<span>{c}</span>{/each}
        <span class="crumbs__file">{n.file}</span>
        <span class="spacer"></span>
        <a class="btn btn--ghost btn--sm" href={`/graph?focus=${encodeURIComponent(handle)}`}>Graph ⌖</a>
        {#if canEdit}
          <button
            class="btn btn--ghost btn--sm star"
            class:star--on={n.favorite}
            onclick={doFavorite}
            disabled={favBusy}
            aria-pressed={n.favorite}
            title={n.favorite ? "Remove from favorites" : "Add to favorites"}
          >{n.favorite ? "★" : "☆"}</button>
          <button class="btn btn--ghost btn--sm" onclick={() => (addingTask = !addingTask)}>＋ Task</button>
          <button class="btn btn--ghost btn--sm" onclick={openMove}>Move</button>
          <button class="btn btn--ghost btn--sm" onclick={doArchive} disabled={archiveBusy}>
            {n.archived ? "Unarchive" : "Archive"}
          </button>
          <button class="btn btn--ghost btn--sm" onclick={() => (editing = true)}>Edit</button>
        {/if}
      </div>
      {#if addingTask}
        <div class="movebar">
          <span class="movebar__label">New task</span>
          <input
            class="movebar__input"
            bind:value={taskText}
            placeholder="what needs doing? (linked to this note)"
            onkeydown={(e) => e.key === "Enter" && doAddTask()}
          />
          <button class="btn btn--sm" onclick={doAddTask} disabled={taskBusy}>{taskBusy ? "Adding…" : "Add task"}</button>
          <button class="btn btn--ghost btn--sm" onclick={() => (addingTask = false)}>Cancel</button>
          {#if taskErr}<span class="error small">{taskErr}</span>{/if}
        </div>
      {/if}
      {#if moving}
        <div class="movebar">
          <span class="movebar__label">Move to folder</span>
          <input
            class="movebar__input"
            list="note-folders"
            bind:value={moveTarget}
            placeholder="(root)"
            onkeydown={(e) => e.key === "Enter" && doMove()}
          />
          <datalist id="note-folders">
            {#each folders as f (f)}<option value={f}></option>{/each}
          </datalist>
          <button class="btn btn--sm" onclick={doMove} disabled={moveBusy}>{moveBusy ? "Moving…" : "Move"}</button>
          <button class="btn btn--ghost btn--sm" onclick={() => (moving = false)}>Cancel</button>
          {#if moveErr}<span class="error small">{moveErr}</span>{/if}
        </div>
      {/if}
      {#if n.archived}
        <div class="archived-banner" role="note">
          <span>📦 Archived — hidden from the sidebar, search, orphans, and the graph. Still on disk.</span>
        </div>
      {/if}
      <h1>{n.title}</h1>
      <div class="note__meta">
        {#if n.source}<span class="src">{n.source}</span>{/if}
        {#if n.created}<span class="muted">{n.created}</span>{/if}
        {#each n.tags as t (t)}<a class="chip" href={`/search?tag=${encodeURIComponent(t)}`}>#{t}</a>{/each}
      </div>

      <!-- bodyHTML is rendered server-side by goldmark (safe mode, escaped). -->
      <div class="prose">{@html n.bodyHTML}</div>

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

      {#if n.backlinks.length}
        <section class="panel">
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

      <nav class="prevnext">
        {#if n.prev}<a href={n.prev.url}>← {n.prev.title}</a>{/if}
        <span class="spacer"></span>
        {#if n.next}<a href={n.next.url}>{n.next.title} →</a>{/if}
      </nav>
    </article>

    {#if toc.length > 1}
      <nav class="toc" aria-label="On this page">
        <div class="toc__head">On this page</div>
        {#each toc as item (item.id)}
          <a
            href={"#" + item.id}
            class="toc__link toc__link--l{item.level}"
            class:active={activeId === item.id}
            onclick={(e) => jump(e, item.id)}>{item.text}</a
          >
        {/each}
      </nav>
    {/if}
  </div>
{/if}

<style>
  /* The favorite star: gold when on, inheriting the ghost-button frame so it
     sits flush with Move/Edit. A touch larger so the glyph reads as a star. */
  .star {
    font-size: 1rem;
    line-height: 1;
  }
  .star--on {
    color: #f5b301;
  }
</style>
