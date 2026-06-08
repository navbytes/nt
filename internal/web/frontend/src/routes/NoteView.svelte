<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import Editor from "../lib/Editor.svelte";

  let { handle, canEdit = false }: { handle: string; canEdit?: boolean } = $props();

  const noteQ = createQuery({ queryKey: ["note", handle], queryFn: () => api.note(handle) });

  let editing = $state(false);
</script>

{#if editing}
  <Editor {handle} onClose={() => (editing = false)} />
{:else if $noteQ.isPending}
  <p class="muted">Loading…</p>
{:else if $noteQ.error}
  <p class="error">Note not found.</p>
{:else if $noteQ.data}
  {@const n = $noteQ.data}
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
{/if}
