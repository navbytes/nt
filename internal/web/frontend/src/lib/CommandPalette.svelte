<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { api, type NoteLink } from "./api";
  import { navigate } from "./router.svelte";
  import { palette, openPalette, closePalette } from "./palette.svelte";

  let q = $state("");
  let active = $state(0);
  let inputEl: HTMLInputElement | undefined = $state();

  const qc = useQueryClient();
  const notesQ = createQuery({ queryKey: ["notes"], queryFn: api.notes });
  const stateQ = createQuery({ queryKey: ["state"], queryFn: api.state });
  const canEdit = $derived($stateQ.data?.canEdit ?? false);

  type Item = { label: string; sub?: string; kind: string; url?: string; run?: () => void };

  const navItems: Item[] = [
    { label: "Dashboard", url: "/", kind: "go" },
    { label: "Tasks", url: "/tasks", kind: "go" },
    { label: "Activity", url: "/activity", kind: "go" },
    { label: "Graph", url: "/graph", kind: "go" },
    { label: "Tags", url: "/tags", kind: "go" },
    { label: "Orphans", url: "/orphans", kind: "go" },
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

  async function newNote() {
    const title = prompt("New note title (use folder/Title for a subfolder):");
    if (!title?.trim()) return;
    const res = await api.noteCreate(title.trim());
    await qc.invalidateQueries({ queryKey: ["notes"] });
    navigate(res.url);
  }

  async function newTask() {
    const text = prompt("New task (try: pay rent due:fri !high @home):");
    if (!text?.trim()) return;
    qc.setQueryData(["tasks"], await api.taskNew(text.trim()));
    navigate("/tasks");
  }

  const actionItems = $derived<Item[]>([
    ...(canEdit
      ? [
          { label: "New note", kind: "action", run: newNote },
          { label: "New task", kind: "action", run: newTask },
        ]
      : []),
    { label: "Toggle theme", kind: "action", run: toggleTheme },
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

  $effect(() => {
    if (palette.open) {
      q = "";
      active = 0;
      queueMicrotask(() => inputEl?.focus());
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
      onclick={(e) => e.stopPropagation()}
    >
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
