<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import Editor from "../lib/Editor.svelte";

  let { handle, canEdit = false }: { handle: string; canEdit?: boolean } = $props();

  const noteQ = createQuery({ queryKey: ["note", handle], queryFn: () => api.note(handle) });

  let editing = $state(false);
  let activeId = $state("");

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

  // Run Mermaid on the rendered note body after mount / on body change.
  $effect(() => {
    const html = $noteQ.data?.bodyHTML;
    if (!html) return;
    // Deferred so the DOM has been updated before we query it.
    const id = setTimeout(async () => {
      const el = document.querySelector(".prose");
      if (!el || !el.querySelector(".mermaid")) return;
      const mermaid = (await import("mermaid")).default;
      const dark = document.documentElement.getAttribute("data-theme") === "dark";
      mermaid.initialize({ startOnLoad: false, theme: dark ? "dark" : "default" });
      await mermaid.run({ nodes: Array.from(el.querySelectorAll(".mermaid")) as HTMLElement[] });
    }, 0);
    return () => clearTimeout(id);
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
        {#if canEdit}<button class="btn btn--ghost btn--sm" onclick={() => (editing = true)}>Edit</button>{/if}
      </div>
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
