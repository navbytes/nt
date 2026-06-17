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
  let drawerRestore: HTMLElement | null = null;
  $effect(() => {
    void path;
    drawerOpen = false;
  });

  // When the drawer opens, move focus into it and trap Tab; Escape closes it and
  // restores focus to the trigger (W13). When it closes, restore focus.
  $effect(() => {
    if (!drawerOpen) return;
    drawerRestore = document.activeElement as HTMLElement;
    const aside = document.querySelector<HTMLElement>(".sidebar");
    queueMicrotask(() => {
      aside?.querySelector<HTMLElement>("a, button, input, [tabindex]")?.focus();
    });
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        e.preventDefault();
        drawerOpen = false;
        return;
      }
      if (e.key !== "Tab" || !aside) return;
      const focusables = [
        ...aside.querySelectorAll<HTMLElement>(
          'a[href], button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])',
        ),
      ].filter((el) => el.offsetParent !== null);
      if (focusables.length === 0) return;
      const first = focusables[0]!;
      const last = focusables[focusables.length - 1]!;
      const active = document.activeElement as HTMLElement;
      if (e.shiftKey && active === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && active === last) {
        e.preventDefault();
        first.focus();
      } else if (!aside.contains(active)) {
        // Focus escaped the drawer (e.g. into the page) — pull it back.
        e.preventDefault();
        first.focus();
      }
    }
    window.addEventListener("keydown", onKey, true);
    return () => {
      window.removeEventListener("keydown", onKey, true);
      drawerRestore?.focus?.();
      drawerRestore = null;
    };
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
  // Re-evaluate when the OS theme changes while no explicit override is set, so
  // the toolbar glyph tracks the system (W27).
  $effect(() => {
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => {
      if (!document.documentElement.getAttribute("data-theme")) isDark = resolveDark();
    };
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
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
    <!-- Ambient aurora: drifting spectral blobs behind all content. The static
         wash lives on .content (app.css); this adds the *motion* the maintainer
         wants. Sits behind content + topbar, never intercepts clicks, and is
         neutralized under reduced-motion (explicit guard for the infinite loop). -->
    <div class="aurora" aria-hidden="true">
      <span class="aurora__blob aurora__blob--1"></span>
      <span class="aurora__blob aurora__blob--2"></span>
    </div>
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

<style>
  /* .content is the positioning context for the aurora layer. Children are ordered
     with explicit z-indexes (aurora 0 < main 1 < topbar 5) so the ambient aurora
     sits behind everything without needing isolation:isolate. (Desktop window-drag
     is handled by Wails reading --wails-draggable off the click target — see
     app.css [data-desktop] — so it's independent of stacking contexts here.) */
  .content {
    position: relative;
  }

  /* The drifting ambient aurora. Absolutely fills the content pane, behind every
     child (z-index:0; the topbar is z-index:5 and main is lifted to z-index:1),
     and passes all clicks through. Two soft spectral blobs ease back and forth
     on the GPU (transform/opacity only). */
  .aurora {
    position: absolute;
    inset: 0;
    z-index: 0;
    overflow: hidden;
    pointer-events: none;
  }
  .aurora__blob {
    position: absolute;
    border-radius: 50%;
    filter: blur(70px);
    /* deliberately faint — an ambient wash, kept low so body text stays AA
       (the blobs also pool in the corners, mostly behind chrome + margins). */
    opacity: 0.28;
    will-change: transform;
  }
  .aurora__blob--1 {
    top: -16%;
    right: -10%;
    width: 46vw;
    max-width: 620px;
    aspect-ratio: 1;
    background: radial-gradient(circle at 50% 50%, var(--spectral-1), transparent 68%);
    animation: aurora-drift var(--aurora-drift) var(--ease) infinite alternate;
  }
  .aurora__blob--2 {
    bottom: -22%;
    left: -14%;
    width: 52vw;
    max-width: 680px;
    aspect-ratio: 1;
    background: radial-gradient(circle at 50% 50%, var(--spectral-3), transparent 68%);
    /* a touch slower + offset so the two blobs never move in lockstep */
    animation: aurora-drift calc(var(--aurora-drift) * 1.35) var(--ease) -8s infinite alternate;
  }
  /* Lift real content above the aurora (topbar already sits at z-index:5). */
  .store-warning,
  .main {
    position: relative;
    z-index: 1;
  }

  /* ── Topbar refinements (behavior unchanged) ──────────────────────────
     Counts speak in the mono "system voice": uppercase, tracked, tabular so
     the digits don't jitter as they update. The strong value keeps full
     contrast; the unit label recedes. */
  .stat {
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    color: var(--label-tertiary);
    font-variant-numeric: tabular-nums;
  }
  .stat strong {
    font-weight: 600;
    color: var(--label-secondary);
  }
  a.stat:hover {
    color: var(--label-secondary);
    text-decoration: none;
  }
  a.stat:hover strong {
    color: var(--spectral-2);
  }
  /* The ⌘K hint reads as a key cap rather than loose text. */
  .palette-btn .kbd {
    padding: 1px 5px;
    border-radius: var(--radius-xs);
    background: var(--fill-strong);
    box-shadow: var(--glass-hairline);
  }

  /* Belt-and-suspenders: the global rule zeroes durations, but for an infinite
     ambient loop we also stop it outright. */
  @media (prefers-reduced-motion: reduce) {
    .aurora__blob {
      animation: none;
    }
  }
</style>
