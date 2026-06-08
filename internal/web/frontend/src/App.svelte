<script lang="ts">
  import { onMount } from "svelte";
  import { api, setCsrf, type State, type TaskGroup } from "./lib/api";

  let appState = $state<State | null>(null);
  let groups = $state<TaskGroup[]>([]);
  let error = $state<string | null>(null);
  let newText = $state("");

  const canEdit = $derived(appState?.canEdit ?? false);

  async function load() {
    try {
      const s = await api.state();
      setCsrf(s.csrf);
      appState = s;
      groups = (await api.tasks()).groups;
    } catch (e) {
      error = String(e);
    }
  }

  async function done(id: string) {
    try {
      groups = (await api.taskDone(id)).groups;
    } catch (e) {
      error = String(e);
    }
  }

  async function add(e: Event) {
    e.preventDefault();
    const text = newText.trim();
    if (!text) return;
    try {
      groups = (await api.taskNew(text)).groups;
      newText = "";
    } catch (err) {
      error = String(err);
    }
  }

  function toggleTheme() {
    const root = document.documentElement;
    const dark =
      root.getAttribute("data-theme") === "dark" ||
      (!root.hasAttribute("data-theme") &&
        window.matchMedia("(prefers-color-scheme: dark)").matches);
    const next = dark ? "light" : "dark";
    root.setAttribute("data-theme", next);
    localStorage.setItem("nt-theme", next);
  }

  onMount(load);
</script>

<header class="topbar">
  <span class="brand">nt</span>
  <span class="spacer"></span>
  {#if appState}
    <span class="stat"><strong>{appState.openCount}</strong> open</span>
    <span class="stat"><strong>{appState.noteCount}</strong> notes</span>
  {/if}
  <button class="icon-btn" onclick={toggleTheme} title="Toggle theme">◐</button>
</header>

<main class="main">
  {#if error}
    <p class="error">Failed to load: {error}</p>
  {:else if !appState}
    <p class="muted">Loading…</p>
  {:else}
    <h1>Memory</h1>
    <p class="muted">
      nt web — Svelte rebuild (preview). {canEdit ? "Editing enabled." : "Read-only."}
    </p>

    {#if canEdit}
      <form class="taskadd" onsubmit={add}>
        <input placeholder="Add a task…" bind:value={newText} autocomplete="off" />
        <button class="btn" type="submit">Add</button>
      </form>
    {/if}

    {#each groups as group (group.status)}
      <section class="group">
        <h2 class="group__title">{group.status} · {group.tasks.length}</h2>
        <ul>
          {#each group.tasks as t (t.id)}
            <li class="row">
              {#if canEdit && t.status !== "done"}
                <button class="check" title="Mark done" onclick={() => done(t.id)}>○</button>
              {/if}
              <span class="row__text">{t.text}</span>
              {#if t.due}<span class="row__due">{t.due}</span>{/if}
              {#if t.source}<span class="src">{t.source}</span>{/if}
            </li>
          {/each}
        </ul>
      </section>
    {:else}
      <p class="muted">No tasks yet.</p>
    {/each}
  {/if}
</main>
