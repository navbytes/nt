<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api, setCsrf } from "./api";
  import { loc, navigate } from "./router.svelte";
  import Sidebar from "./Sidebar.svelte";
  import Home from "../routes/Home.svelte";
  import NoteView from "../routes/NoteView.svelte";
  import Tasks from "../routes/Tasks.svelte";
  import Activity from "../routes/Activity.svelte";
  import Search from "../routes/Search.svelte";
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
  <Sidebar {path} />

  <div class="content">
    <header class="topbar">
      <form class="topbar__search" onsubmit={onSearch}>
        <input name="q" placeholder="Search notes…" autocomplete="off" />
      </form>
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
        {#key loc.query.get("q")}
          <Search q={loc.query.get("q") ?? ""} />
        {/key}
      {:else}
        <NotFound {path} />
      {/if}
    </main>
  </div>
</div>
