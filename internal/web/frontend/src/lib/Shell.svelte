<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api, setCsrf } from "./api";
  import { loc, navigate } from "./router.svelte";
  import Sidebar from "./Sidebar.svelte";
  import SidebarResizer from "./SidebarResizer.svelte";
  import { sidebar, toggleCollapsed } from "./sidebarState.svelte";
  import SearchBox from "./SearchBox.svelte";
  import CommandPalette from "./CommandPalette.svelte";
  import Shortcuts from "./Shortcuts.svelte";
  import Toast from "./Toaster.svelte";
  import About from "./AboutDialog.svelte";
  import { openPalette } from "./palette.svelte";
  import { shortcuts } from "./keys.svelte";
  import Icon from "./Icon.svelte";
  import Home from "../routes/Home.svelte";
  import NoteView from "../routes/NoteView.svelte";
  import Tasks from "../routes/Tasks.svelte";
  import Activity from "../routes/Activity.svelte";
  import Search from "../routes/Search.svelte";
  import Tags from "../routes/Tags.svelte";
  import Graph from "../routes/Graph.svelte";
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

  // Track the resolved theme so the toolbar toggle shows the right glyph
  // (sun when dark → tap for light; moon when light → tap for dark).
  let isDark = $state(false);
  function resolveDark(): boolean {
    const root = document.documentElement;
    const attr = root.getAttribute("data-theme");
    if (attr === "dark") return true;
    if (attr === "light") return false;
    return window.matchMedia("(prefers-color-scheme: dark)").matches;
  }
  $effect(() => {
    isDark = resolveDark();
  });

  function toggleTheme() {
    const root = document.documentElement;
    const next = resolveDark() ? "light" : "dark";
    root.setAttribute("data-theme", next);
    localStorage.setItem("nt-theme", next);
    isDark = next === "dark";
    // keep the PWA/status-bar tint in sync
    document
      .querySelector('meta[name="theme-color"]')
      ?.setAttribute("content", next === "dark" ? "#1e1e20" : "#ffffff");
  }

</script>

<a href="#main-content" class="skip-link">Skip to content</a>
<div class="layout" class:sidebar-collapsed={sidebar.collapsed} style="--sidebar-w: {sidebar.width}px">
  <Sidebar {path} open={drawerOpen} />
  <SidebarResizer />
  {#if drawerOpen}
    <div class="scrim" role="presentation" onclick={() => (drawerOpen = false)}></div>
  {/if}

  <div class="content">
    <header class="topbar">
      <button
        class="hamburger"
        onclick={() => (drawerOpen = !drawerOpen)}
        aria-label="Toggle navigation menu"
        aria-expanded={drawerOpen}><Icon name="sidebar" /></button
      >
      <button
        class="collapse-btn"
        onclick={toggleCollapsed}
        title="Toggle sidebar ([)"
        aria-label="Toggle sidebar"
        aria-expanded={!sidebar.collapsed}><Icon name="sidebar" /></button
      >
      <div class="topbar__search"><SearchBox /></div>
      <button
        class="palette-btn"
        onclick={openPalette}
        title="Command palette (⌘K)"
        aria-haspopup="dialog"
        aria-label="Open command palette"
      >
        <Icon name="search" size={14} />
        <span class="kbd">⌘K</span>
      </button>
      <span class="spacer"></span>
      {#if $stateQ.data}
        <a class="stat" href="/tasks"><strong>{$stateQ.data.openCount}</strong> open</a>
        <span class="stat"><strong>{$stateQ.data.noteCount}</strong> notes</span>
      {/if}
      <button
        class="icon-btn"
        onclick={() => (shortcuts.open = true)}
        title="Keyboard shortcuts (?)"
        aria-haspopup="dialog"
        aria-expanded={shortcuts.open}
        aria-label="Keyboard shortcuts"><Icon name="help" /></button
      >
      <button
        class="icon-btn"
        onclick={toggleTheme}
        title="Toggle theme"
        aria-pressed={isDark}
        aria-label="Toggle light/dark theme"><Icon name={isDark ? "sun" : "moon"} /></button
      >
    </header>

    {#if $stateQ.data?.warning}
      <div class="store-warning" role="alert">
        <Icon name="warning" size={15} />
        {$stateQ.data.warning}
      </div>
    {/if}

    <main class="main" id="main-content" tabindex="-1">
      {#if path === "/"}
        <Home />
      {:else if noteHandle}
        {#key noteHandle}
          <NoteView handle={noteHandle} />
        {/key}
      {:else if path === "/tasks"}
        {#key loc.query.get("view") ?? ""}
          <Tasks viewName={loc.query.get("view") ?? ""} />
        {/key}
      {:else if path === "/review"}
        <Tasks initialView="review" />
      {:else if path === "/notes" || path === "/journal"}
        <Notes />
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
  <button class="fab" aria-label="New task" title="New task" onclick={() => navigate("/tasks")}
    ><Icon name="plus" size={22} /></button
  >

  <CommandPalette />
  <Shortcuts />
  <Toast />
  <About />
</div>
