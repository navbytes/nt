<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { api, type NoteLink } from "./api";
  import { navigate, loc } from "./router.svelte";
  import { palette, openPalette, closePalette } from "./palette.svelte";
  import { requestMoveNote, requestNewNote } from "./noteUI.svelte";
  import { captureTask } from "./keys.svelte";
  import { openAbout } from "./about.svelte";
  import { toggleCollapsed } from "./sidebarState.svelte";
  import Icon from "./Icon.svelte";

  let q = $state("");
  let active = $state(0);
  let inputEl: HTMLInputElement | undefined = $state();

  const notesQ = createQuery({ queryKey: ["notes"], queryFn: api.notes });

  type Item = { label: string; sub?: string; kind: string; url?: string; run?: () => void };

  const navItems: Item[] = [
    { label: "Today", url: "/", kind: "go" },
    { label: "Tasks", url: "/tasks", kind: "go" },
    { label: "Review (triage)", url: "/review", kind: "go" },
    { label: "Notes", url: "/notes", kind: "go" },
    { label: "Daily note (today)", url: "/journal", kind: "go" },
    { label: "Graph", url: "/graph", kind: "go" },
    { label: "Activity", url: "/activity", kind: "go" },
    { label: "Tags", url: "/tags", kind: "go" },
  ];

  function toggleTheme() {
    const root = document.documentElement;
    const dark =
      root.getAttribute("data-theme") === "dark" ||
      (!root.hasAttribute("data-theme") && window.matchMedia("(prefers-color-scheme: dark)").matches);
    const next = dark ? "light" : "dark";
    root.setAttribute("data-theme", next);
    try {
      localStorage.setItem("nt-theme", next);
    } catch {
      /* private mode */
    }
  }

  // No prompt() dialogs — webviews (the desktop app) don't implement them, and
  // inline inputs are better UX anyway. "New note" opens the Sidebar's inline
  // input; "New task" drops the cursor in a quick-add box.
  function newNote() {
    requestNewNote();
  }

  function newTask() {
    captureTask();
  }

  const onNotePage = $derived(loc.path.startsWith("/n/"));
  const actionItems = $derived<Item[]>([
    { label: "New note", kind: "action", run: newNote },
    { label: "New task", kind: "action", run: newTask },
    ...(onNotePage ? [{ label: "Move note…", kind: "action", run: requestMoveNote }] : []),
    { label: "Toggle sidebar", kind: "action", run: toggleCollapsed },
    { label: "Toggle theme", kind: "action", run: toggleTheme },
    { label: "About nt", kind: "action", run: openAbout },
  ]);

  const items = $derived(build(q, $notesQ.data?.index ?? [], actionItems));

  function build(query: string, index: NoteLink[], actions: Item[]): Item[] {
    const ql = query.trim().toLowerCase();
    const match = (s: string) => s.toLowerCase().includes(ql);
    const nav = navItems.filter((n) => !ql || match(n.label));
    const acts = actions.filter((a) => !ql || match(a.label));
    const notes: Item[] = index
      .filter((n) => !ql || match(n.title) || match(n.path))
      .slice(0, 8)
      .map((n) => ({ label: n.title, sub: n.path, url: n.url, kind: "note" }));
    const out = [...nav, ...acts, ...notes];
    if (ql)
      out.push({
        label: `Search notes for “${query}”`,
        url: `/search?q=${encodeURIComponent(query)}`,
        kind: "search",
      });
    return out;
  }

  $effect(() => {
    if (active > items.length - 1) active = Math.max(0, items.length - 1);
  });

  // Move focus into the palette on open and restore it to the trigger on close
  // (modal focus management).
  let restoreFocus: HTMLElement | null = null;
  $effect(() => {
    if (palette.open) {
      restoreFocus = document.activeElement as HTMLElement;
      q = "";
      active = 0;
      queueMicrotask(() => inputEl?.focus());
    } else if (restoreFocus) {
      restoreFocus.focus?.();
      restoreFocus = null;
    }
  });

  function choose(it: Item | undefined) {
    if (!it) return;
    closePalette();
    if (it.run) it.run();
    else if (it.url) navigate(it.url);
  }

  function onGlobalKey(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && (e.key === "k" || e.key === "K")) {
      e.preventDefault();
      palette.open ? closePalette() : openPalette();
    }
  }

  function onInputKey(e: KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      active = Math.min(active + 1, items.length - 1);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      active = Math.max(active - 1, 0);
    } else if (e.key === "Enter") {
      e.preventDefault();
      choose(items[active]);
    } else if (e.key === "Escape") {
      e.preventDefault();
      closePalette();
    } else if (e.key === "Tab") {
      // Trap focus: the list is navigated with the arrow keys (aria-activedescendant),
      // so Tab keeps focus on the input rather than escaping behind the backdrop.
      e.preventDefault();
    }
  }

  $effect(() => {
    window.addEventListener("keydown", onGlobalKey);
    return () => window.removeEventListener("keydown", onGlobalKey);
  });
</script>

{#if palette.open}
  <div class="palette__backdrop" onclick={closePalette} role="presentation">
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div
      class="palette"
      role="dialog"
      aria-modal="true"
      aria-label="Command palette"
      tabindex="-1"
      onclick={(e) => e.stopPropagation()}
    >
      <div class="palette__search">
        <Icon name="search" size={16} />
        <input
          bind:this={inputEl}
          bind:value={q}
          onkeydown={onInputKey}
          class="palette__input"
          placeholder="Search notes, run a command, jump to…"
          autocomplete="off"
          spellcheck="false"
          role="combobox"
          aria-expanded="true"
          aria-controls="palette-listbox"
          aria-activedescendant={items.length ? `palette-opt-${active}` : undefined}
        />
      </div>
      <ul class="palette__list" id="palette-listbox" role="listbox">
        {#each items as it, i (it.kind + (it.url ?? it.label))}
          <li role="presentation">
            <button
              id={`palette-opt-${i}`}
              role="option"
              aria-selected={i === active}
              class="palette__item"
              class:active={i === active}
              onmouseenter={() => (active = i)}
              onclick={() => choose(it)}
            >
              <span class="palette__kind">{it.kind}</span>
              <span class="palette__label">{it.label}</span>
              {#if it.sub}<span class="palette__sub">{it.sub}</span>{/if}
            </button>
          </li>
        {:else}
          <li class="palette__empty muted" role="presentation">No matches</li>
        {/each}
      </ul>
      <div class="palette__hint muted">↑↓ navigate · ↵ run · esc close</div>
    </div>
  </div>
{/if}

<style>
  /* Leading search glyph in the input row. The palette container, input, list,
     and hint styling all live in app.css; this only positions the new icon and
     indents the field so its text clears the glyph. */
  .palette__search {
    position: relative;
  }
  .palette__search :global(.icon) {
    position: absolute;
    left: 18px;
    top: 50%;
    transform: translateY(-50%);
    color: var(--muted);
  }
  .palette__search :global(.palette__input) {
    padding-left: 44px;
  }
  /* The field is auto-focused when the palette opens; the modal context + the
     hairline underline are affordance enough, so suppress the global focus halo
     here to avoid a jarring ring on open (app.css already sets outline:none). */
  .palette__search :global(.palette__input:focus-visible) {
    box-shadow: none;
  }
</style>
