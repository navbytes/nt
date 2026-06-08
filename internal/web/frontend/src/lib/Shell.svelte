<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api, setCsrf } from "./api";
  import { loc, navigate } from "./router.svelte";
  import Sidebar from "./Sidebar.svelte";
  import CommandPalette from "./CommandPalette.svelte";
  import { openPalette } from "./palette.svelte";
  import Home from "../routes/Home.svelte";
  import NoteView from "../routes/NoteView.svelte";
  import Tasks from "../routes/Tasks.svelte";
  import Activity from "../routes/Activity.svelte";
  import Search from "../routes/Search.svelte";
  import Tags from "../routes/Tags.svelte";
  import Orphans from "../routes/Orphans.svelte";
  import Graph from "../routes/Graph.svelte";
  import NotFound from "../routes/NotFound.svelte";

  const stateQ = createQuery({ queryKey: ["state"], queryFn: api.state });

  // Feed the CSRF token to the API client whenever state (re)loads.
  $effect(() => {
    if ($stateQ.data) setCsrf($stateQ.data.csrf);
  });

  const path = $derived(loc.path);
  const noteHandle = $derived(
    path.startsWith("/n/") ? decodeURIComponent(path.slice(3)) : "",
  );

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

  function onSearch(e: SubmitEvent) {
    e.preventDefault();
    const q = new FormData(e.target as HTMLFormElement).get("q") as string;
    navigate(`/search?q=${encodeURIComponent(q ?? "")}`);
  }
</script>

<div class="layout">
  <Sidebar {path} canEdit={$stateQ.data?.canEdit ?? false} />

  <div class="content">
    <header class="topbar">
      <form class="topbar__search" onsubmit={onSearch}>
        <input name="q" placeholder="Search notes…" autocomplete="off" />
      </form>
      <button class="palette-btn" onclick={openPalette} title="Command palette (⌘K)">
        <span class="kbd">⌘K</span>
      </button>
      <span class="spacer"></span>
      {#if $stateQ.data}
        <a class="stat" href="/tasks"><strong>{$stateQ.data.openCount}</strong> open</a>
        <span class="stat"><strong>{$stateQ.data.noteCount}</strong> notes</span>
        {#if $stateQ.data.canEdit}<span class="badge">edit</span>{/if}
      {/if}
      <button class="icon-btn" onclick={toggleTheme} title="Toggle theme">◐</button>
    </header>

    <main class="main">
      {#if path === "/"}
        <Home canEdit={$stateQ.data?.canEdit ?? false} />
      {:else if noteHandle}
        {#key noteHandle}
          <NoteView handle={noteHandle} canEdit={$stateQ.data?.canEdit ?? false} />
        {/key}
      {:else if path === "/tasks"}
        <Tasks canEdit={$stateQ.data?.canEdit ?? false} />
      {:else if path === "/activity"}
        <Activity />
      {:else if path === "/search"}
        {#key (loc.query.get("q") ?? "") + "|" + (loc.query.get("tag") ?? "")}
          <Search q={loc.query.get("q") ?? ""} tag={loc.query.get("tag") ?? ""} />
        {/key}
      {:else if path === "/tags"}
        <Tags />
      {:else if path === "/orphans"}
        <Orphans />
      {:else if path === "/graph"}
        {#key loc.query.get("focus") ?? ""}
          <Graph focus={loc.query.get("focus") ?? ""} />
        {/key}
      {:else}
        <NotFound {path} />
      {/if}
    </main>
  </div>

  <CommandPalette />
</div>
