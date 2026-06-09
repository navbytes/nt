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
  import Graph from "../routes/Graph.svelte";
  import Journal from "../routes/Journal.svelte";
  import Notes from "../routes/Notes.svelte";
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

  // Mobile nav drawer: closed by default, auto-closes on navigation.
  let drawerOpen = $state(false);
  $effect(() => {
    void path;
    drawerOpen = false;
  });

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

<a href="#main-content" class="skip-link">Skip to content</a>
<div class="layout">
  <Sidebar {path} canEdit={$stateQ.data?.canEdit ?? false} open={drawerOpen} />
  {#if drawerOpen}
    <div class="scrim" role="presentation" onclick={() => (drawerOpen = false)}></div>
  {/if}

  <div class="content">
    <header class="topbar">
      <button
        class="hamburger"
        onclick={() => (drawerOpen = !drawerOpen)}
        aria-label="Toggle navigation menu"
        aria-expanded={drawerOpen}>☰</button
      >
      <form class="topbar__search" onsubmit={onSearch}>
        <input name="q" placeholder="Search notes…" autocomplete="off" />
      </form>
      <button class="palette-btn" onclick={openPalette} title="Command palette (⌘K)" aria-label="Open command palette">
        <span class="kbd">⌘K</span>
      </button>
      <span class="spacer"></span>
      {#if $stateQ.data}
        <a class="stat" href="/tasks"><strong>{$stateQ.data.openCount}</strong> open</a>
        <span class="stat"><strong>{$stateQ.data.noteCount}</strong> notes</span>
        {#if $stateQ.data.canEdit}<span class="badge">edit</span>{/if}
      {/if}
      <button class="icon-btn" onclick={toggleTheme} title="Toggle theme" aria-label="Toggle light/dark theme">◐</button>
    </header>

    {#if $stateQ.data?.warning}
      <div class="store-warning" role="alert">⚠ {$stateQ.data.warning}</div>
    {/if}

    <main class="main" id="main-content" tabindex="-1">
      {#if path === "/"}
        <Home canEdit={$stateQ.data?.canEdit ?? false} />
      {:else if noteHandle}
        {#key noteHandle}
          <NoteView handle={noteHandle} canEdit={$stateQ.data?.canEdit ?? false} />
        {/key}
      {:else if path === "/tasks"}
        <Tasks canEdit={$stateQ.data?.canEdit ?? false} />
      {:else if path === "/review"}
        <Tasks canEdit={$stateQ.data?.canEdit ?? false} initialView="review" />
      {:else if path === "/notes"}
        <Notes />
      {:else if path === "/journal"}
        {#key loc.query.get("date") ?? ""}
          <Journal date={loc.query.get("date") ?? ""} canEdit={$stateQ.data?.canEdit ?? false} />
        {/key}
      {:else if path === "/activity"}
        <Activity />
      {:else if path === "/search"}
        {#key (loc.query.get("q") ?? "") + "|" + (loc.query.get("tag") ?? "")}
          <Search q={loc.query.get("q") ?? ""} tag={loc.query.get("tag") ?? ""} />
        {/key}
      {:else if path === "/tags"}
        <Tags />
      {:else if path === "/graph"}
        {#key loc.query.get("focus") ?? ""}
          <Graph focus={loc.query.get("focus") ?? ""} />
        {/key}
      {:else}
        <NotFound {path} />
      {/if}
    </main>
  </div>

  <!-- Mobile-only capture affordance: thumb-reachable, jumps to the task add. -->
  <button class="fab" aria-label="New task" title="New task" onclick={() => navigate("/tasks")}>＋</button>

  <CommandPalette />
</div>
