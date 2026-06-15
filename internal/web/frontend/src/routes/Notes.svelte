<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import type { NoteCard } from "../lib/api";
  import { loc, navigate } from "../lib/router.svelte";
  import { showToast } from "../lib/toast.svelte";
  import Journal from "./Journal.svelte";
  import Icon from "../lib/Icon.svelte";

  // Daily (journal) is a view of Notes, selected by the /journal path. The grid
  // is the default; the header toggle switches between them.
  const daily = $derived(loc.path === "/journal");

  const gridQ = createQuery({ queryKey: ["notesGrid"], queryFn: api.notesGrid });
  // Orphans (notes with no links in or out) fold in here as a filter.
  const orphansQ = createQuery({ queryKey: ["orphans"], queryFn: api.orphans });
  const orphanUrls = $derived(new Set(($orphansQ.data?.notes ?? []).map((n) => n.url)));

  const archivedCount = $derived(($gridQ.data?.notes ?? []).filter((n) => n.archived).length);
  const favoriteCount = $derived(
    ($gridQ.data?.notes ?? []).filter((n) => n.favorite && !n.archived).length,
  );

  type Layout = "cards" | "compact" | "list" | "timeline";
  type Sort = "updated" | "created" | "title" | "folder" | "tags";
  type GroupBy = "none" | "folder" | "tag" | "recency";
  type Recency = "all" | "7" | "30" | "90";

  // View prefs persist (like the old dense/sort did); content filters stay
  // ephemeral — they reset on reload, matching the previous behaviour.
  let layout = $state<Layout>((localStorage.getItem("nt-notes-layout") as Layout) ?? "cards");
  let sort = $state<Sort>((localStorage.getItem("nt-notes-sort") as Sort) ?? "updated");
  let sortDir = $state<"asc" | "desc">(
    (localStorage.getItem("nt-notes-sortdir") as "asc" | "desc") ?? "desc",
  );
  let groupBy = $state<GroupBy>((localStorage.getItem("nt-notes-group") as GroupBy) ?? "none");
  $effect(() => localStorage.setItem("nt-notes-layout", layout));
  $effect(() => localStorage.setItem("nt-notes-sort", sort));
  $effect(() => localStorage.setItem("nt-notes-sortdir", sortDir));
  $effect(() => localStorage.setItem("nt-notes-group", groupBy));

  // Content filters (ephemeral).
  let folder = $state("");
  let q = $state("");
  let activeTags = $state<string[]>([]);
  let recency = $state<Recency>("all");
  let orphansOnly = $state(false);
  let archivedOnly = $state(false);
  let favoritesOnly = $state(false);

  const hasFilters = $derived(
    !!folder ||
      !!q.trim() ||
      activeTags.length > 0 ||
      recency !== "all" ||
      orphansOnly ||
      archivedOnly ||
      favoritesOnly,
  );
  function clearFilters(): void {
    folder = "";
    q = "";
    activeTags = [];
    recency = "all";
    orphansOnly = false;
    archivedOnly = false;
    favoritesOnly = false;
  }

  // Inline "New note" creation right on the grid — mirrors the sidebar's flow
  // (an in-place field, not prompt(), which webviews don't support) and creates
  // into the active folder filter so it lands where you're looking.
  const qc = useQueryClient();
  let newOpen = $state(false);
  let newTitle = $state("");
  let newInput: HTMLInputElement | undefined = $state();
  let creating = $state(false);
  function openNew(): void {
    newOpen = true;
    queueMicrotask(() => newInput?.focus());
  }
  async function submitNew(e: SubmitEvent): Promise<void> {
    e.preventDefault();
    const title = newTitle.trim();
    if (!title || creating) return;
    creating = true;
    try {
      const res = await api.noteCreate(title, folder); // folder "" = root
      await qc.invalidateQueries({ queryKey: ["notesGrid"] });
      await qc.invalidateQueries({ queryKey: ["notes"] });
      newTitle = "";
      newOpen = false;
      navigate(res.url);
    } catch (err) {
      showToast(`Couldn't create the note: ${String(err)}`);
    } finally {
      creating = false;
    }
  }
  function onNewKey(e: KeyboardEvent): void {
    if (e.key === "Escape") {
      e.preventDefault();
      newOpen = false;
      newTitle = "";
    }
  }

  // Tag vocabulary (working set), for the "+ Tag" picker — sorted alphabetically.
  const allTags = $derived.by(() => {
    const seen = new Set<string>();
    for (const n of $gridQ.data?.notes ?? []) {
      if (n.archived) continue;
      for (const t of n.tags ?? []) seen.add(t);
    }
    return [...seen].sort((a, b) => a.localeCompare(b));
  });

  function toggleTag(t: string, e?: Event): void {
    e?.preventDefault();
    e?.stopPropagation();
    activeTags = activeTags.includes(t) ? activeTags.filter((x) => x !== t) : [...activeTags, t];
  }
  function addTag(t: string): void {
    if (t && !activeTags.includes(t)) activeTags = [...activeTags, t];
  }
  // Sort keys read best in different directions; snap to a sane default on change.
  function pickSort(k: Sort): void {
    sort = k;
    sortDir = k === "title" || k === "folder" ? "asc" : "desc";
  }
  function cutoff(days: number): string {
    const d = new Date();
    d.setDate(d.getDate() - days);
    return d.toISOString().slice(0, 10);
  }

  const filtered = $derived.by((): NoteCard[] => {
    let ns = [...($gridQ.data?.notes ?? [])];
    // Default view is the working set; the Archived toggle flips to retired only.
    ns = ns.filter((n) => (archivedOnly ? n.archived : !n.archived));
    if (folder) ns = ns.filter((n) => n.folder === folder || n.folder.startsWith(folder + "/"));
    if (orphansOnly) ns = ns.filter((n) => orphanUrls.has(n.url));
    if (favoritesOnly) ns = ns.filter((n) => n.favorite);
    // Multi-tag is AND: a note must carry every selected tag.
    if (activeTags.length) ns = ns.filter((n) => activeTags.every((t) => n.tags?.includes(t)));
    if (recency !== "all") {
      const c = cutoff(Number(recency));
      ns = ns.filter((n) => (n.updated ?? "") >= c);
    }
    const needle = q.trim().toLowerCase();
    if (needle) {
      ns = ns.filter(
        (n) =>
          n.title.toLowerCase().includes(needle) ||
          (n.preview ?? "").toLowerCase().includes(needle) ||
          (n.tags ?? []).some((t) => t.toLowerCase().includes(needle)),
      );
    }
    const dir = sortDir === "desc" ? -1 : 1;
    ns.sort((a, b) => {
      let r = 0;
      if (sort === "title") r = a.title.localeCompare(b.title);
      else if (sort === "folder")
        r = a.folder.localeCompare(b.folder) || a.title.localeCompare(b.title);
      else if (sort === "tags") r = (a.tags?.length ?? 0) - (b.tags?.length ?? 0);
      else if (sort === "created") r = (a.created ?? "").localeCompare(b.created ?? "");
      else r = (a.updated ?? "").localeCompare(b.updated ?? "");
      return (r || a.title.localeCompare(b.title)) * dir;
    });
    return ns;
  });

  // Grouping splits the filtered+sorted list into labelled sections. A note can
  // appear under several tag groups (one per tag); folder/recency are exclusive.
  type Group = { key: string; label: string; cards: NoteCard[] };
  function push(m: Map<string, NoteCard[]>, k: string, n: NoteCard): void {
    const a = m.get(k);
    if (a) a.push(n);
    else m.set(k, [n]);
  }
  const groups = $derived.by((): Group[] => {
    const ns = filtered;
    if (groupBy === "none") return [{ key: "", label: "", cards: ns }];
    if (groupBy === "folder") {
      const m = new Map<string, NoteCard[]>();
      for (const n of ns) push(m, n.folder || "(root)", n);
      return [...m.entries()]
        .sort((a, b) =>
          a[0] === "(root)" ? -1 : b[0] === "(root)" ? 1 : a[0].localeCompare(b[0]),
        )
        .map(([k, cards]) => ({ key: k, label: k, cards }));
    }
    if (groupBy === "tag") {
      const m = new Map<string, NoteCard[]>();
      for (const n of ns) {
        const ts = n.tags?.length ? n.tags : ["(untagged)"];
        for (const t of ts) push(m, t, n);
      }
      return [...m.entries()]
        .sort((a, b) =>
          a[0] === "(untagged)" ? 1 : b[0] === "(untagged)" ? -1 : a[0].localeCompare(b[0]),
        )
        .map(([k, cards]) => ({ key: k, label: k === "(untagged)" ? k : "#" + k, cards }));
    }
    // recency
    const c7 = cutoff(7);
    const c30 = cutoff(30);
    const today = new Date().toISOString().slice(0, 10);
    const order = ["Today", "Past week", "Past month", "Older", "Undated"];
    const m = new Map<string, NoteCard[]>();
    for (const n of ns) {
      const d = n.updated ?? "";
      const k = !d
        ? "Undated"
        : d >= today
          ? "Today"
          : d >= c7
            ? "Past week"
            : d >= c30
              ? "Past month"
              : "Older";
      push(m, k, n);
    }
    return order.filter((k) => m.has(k)).map((k) => ({ key: k, label: k, cards: m.get(k)! }));
  });

  const total = $derived(filtered.length);

  // Timeline view: notes laid out chronologically, grouped by month. The axis is
  // the date you're sorting by (Created, else Updated/last-touched); the sort
  // direction toggle flips oldest↔newest.
  const MONTHS = [
    "January", "February", "March", "April", "May", "June",
    "July", "August", "September", "October", "November", "December",
  ];
  function monthLabel(yyyymm: string): string {
    const [y, m] = yyyymm.split("-");
    return (MONTHS[Number(m) - 1] ?? m) + " " + y;
  }
  type TLEntry = { card: NoteCard; date: string };
  type TLMonth = { key: string; label: string; entries: TLEntry[] };
  const timeline = $derived.by((): TLMonth[] => {
    const useCreated = sort === "created";
    const dateOf = (c: NoteCard): string => (useCreated ? c.created : c.updated) ?? "";
    const dir = sortDir === "desc" ? -1 : 1;
    const dated = filtered
      .filter((c) => dateOf(c))
      .map((c): TLEntry => ({ card: c, date: dateOf(c) }))
      .sort((a, b) => (a.date < b.date ? -1 : a.date > b.date ? 1 : 0) * dir);
    const m = new Map<string, TLEntry[]>();
    for (const e of dated) {
      const mk = e.date.slice(0, 7); // YYYY-MM
      const a = m.get(mk);
      if (a) a.push(e);
      else m.set(mk, [e]);
    }
    const months: TLMonth[] = [...m.entries()].map(([key, entries]) => ({
      key,
      label: monthLabel(key),
      entries,
    }));
    const undated = filtered.filter((c) => !dateOf(c));
    if (undated.length)
      months.push({
        key: "undated",
        label: "Undated",
        entries: undated.map((c): TLEntry => ({ card: c, date: "" })),
      });
    return months;
  });
</script>

<div class="pagehead">
  <div class="notes-head">
    <h1>Notes</h1>
    <div class="seg" role="group" aria-label="Notes view">
      <button class:seg--on={!daily} aria-pressed={!daily} onclick={() => navigate("/notes")}>All</button>
      <button class:seg--on={daily} aria-pressed={daily} onclick={() => navigate("/journal")}>Daily</button>
    </div>
    {#if !daily}
      {#if newOpen}
        <form class="newnote" onsubmit={submitNew}>
          <input
            bind:this={newInput}
            bind:value={newTitle}
            onkeydown={onNewKey}
            placeholder={folder ? `New note in ${folder}/…` : "New note title…"}
            aria-label="New note title"
            autocomplete="off"
            disabled={creating}
          />
          <button type="submit" class="newnote__go" disabled={creating || !newTitle.trim()}>Create</button>
          <button
            type="button"
            class="newnote__x"
            onclick={() => {
              newOpen = false;
              newTitle = "";
            }}
            aria-label="Cancel"><Icon name="close" size={15} /></button>
        </form>
      {:else}
        <button class="newnote__open" onclick={openNew}><Icon name="plus" size={14} /> New note</button>
      {/if}
    {/if}
  </div>

  {#if !daily && $gridQ.data}
    <div class="notes-controls">
      <select class="select" bind:value={folder} aria-label="Filter by folder">
        <option value="">All folders</option>
        {#each $gridQ.data.folders as f (f)}<option value={f}>{f}</option>{/each}
      </select>

      {#if allTags.length}
        <select
          class="select"
          aria-label="Add tag filter"
          onchange={(e) => {
            const sel = e.currentTarget as HTMLSelectElement;
            addTag(sel.value);
            sel.value = "";
          }}
        >
          <option value="">＋ Tag…</option>
          {#each allTags as t (t)}
            {#if !activeTags.includes(t)}<option value={t}>#{t}</option>{/if}
          {/each}
        </select>
      {/if}

      <select class="select" bind:value={recency} aria-label="Filter by recency">
        <option value="all">Any time</option>
        <option value="7">Past week</option>
        <option value="30">Past month</option>
        <option value="90">Past 90 days</option>
      </select>

      <div class="sortwrap">
        <select
          class="select"
          value={sort}
          aria-label="Sort notes"
          onchange={(e) => pickSort((e.currentTarget as HTMLSelectElement).value as Sort)}
        >
          <option value="updated">Updated</option>
          <option value="created">Created</option>
          <option value="title">Title</option>
          <option value="folder">Folder</option>
          <option value="tags">Tag count</option>
        </select>
        <button
          class="dirbtn"
          title={sortDir === "desc" ? "Descending — click for ascending" : "Ascending — click for descending"}
          aria-label="Toggle sort direction"
          onclick={() => (sortDir = sortDir === "desc" ? "asc" : "desc")}
        ><Icon name={sortDir === "desc" ? "arrow-down" : "arrow-up"} size={14} /></button>
      </div>

      {#if layout !== "timeline"}
        <select class="select" bind:value={groupBy} aria-label="Group notes">
          <option value="none">No grouping</option>
          <option value="folder">Group: Folder</option>
          <option value="tag">Group: Tag</option>
          <option value="recency">Group: Recency</option>
        </select>
      {/if}

      <div class="seg" role="group" aria-label="Layout">
        <button class:seg--on={layout === "cards"} aria-pressed={layout === "cards"} onclick={() => (layout = "cards")}>Cards</button>
        <button class:seg--on={layout === "compact"} aria-pressed={layout === "compact"} onclick={() => (layout = "compact")}>Compact</button>
        <button class:seg--on={layout === "list"} aria-pressed={layout === "list"} onclick={() => (layout = "list")}>List</button>
        <button
          class:seg--on={layout === "timeline"}
          aria-pressed={layout === "timeline"}
          onclick={() => {
            layout = "timeline";
            if (sort !== "created" && sort !== "updated") pickSort("updated");
          }}>Timeline</button>
      </div>

      {#if favoriteCount > 0 || favoritesOnly}
        <button
          class="notes-toggle"
          class:notes-toggle--on={favoritesOnly}
          aria-pressed={favoritesOnly}
          title="Show only starred notes"
          onclick={() => (favoritesOnly = !favoritesOnly)}
        ><Icon name="star" filled={favoritesOnly} size={14} /> Favorites{#if favoriteCount}<span class="notes-toggle__count"> {favoriteCount}</span>{/if}</button>
      {/if}
      <button
        class="notes-toggle"
        class:notes-toggle--on={orphansOnly}
        aria-pressed={orphansOnly}
        title="Show only notes with no links in or out"
        onclick={() => (orphansOnly = !orphansOnly)}
      >Orphans{#if $orphansQ.data?.notes.length}<span class="notes-toggle__count"> {$orphansQ.data.notes.length}</span>{/if}</button>
      {#if archivedCount > 0 || archivedOnly}
        <button
          class="notes-toggle"
          class:notes-toggle--on={archivedOnly}
          aria-pressed={archivedOnly}
          title="Show retired notes (hidden from the sidebar, search, and graph)"
          onclick={() => (archivedOnly = !archivedOnly)}
        ><Icon name="archive" size={14} /> Archived{#if archivedCount}<span class="notes-toggle__count"> {archivedCount}</span>{/if}</button>
      {/if}
    </div>

    <div class="notes-filterrow">
      <div class="qfilter">
        <input
          bind:value={q}
          onkeydown={(e) => e.key === "Escape" && (q = "")}
          placeholder="Filter notes by title, text, or #tag…"
          aria-label="Filter notes"
          autocomplete="off"
        />
        {#if q}<button class="qfilter__clear" onclick={() => (q = "")} aria-label="Clear text filter"><Icon name="close" size={14} /></button>{/if}
      </div>
      {#if activeTags.length}
        <div class="tagbar" role="group" aria-label="Active tag filters">
          {#each activeTags as t (t)}
            <button class="chip chip--active" onclick={() => toggleTag(t)} title="Remove this tag filter" aria-label={`Remove tag filter #${t}`}>#{t} <Icon name="close" size={12} /></button>
          {/each}
        </div>
      {/if}
      {#if hasFilters}
        <button class="notes-clear" onclick={clearFilters}>Clear all</button>
      {/if}
    </div>
  {/if}
</div>

{#if daily}
  <Journal />
{:else if $gridQ.isPending}
  <p class="muted">Loading…</p>
{:else if $gridQ.error}
  <div class="empty">
    <p class="empty__lead">Couldn't load notes.</p>
    <p class="muted">Something went wrong reaching the store. Check that <code>nt</code> is running, then try again.</p>
    <button class="btn btn--ghost btn--sm" onclick={() => $gridQ.refetch()}>Try again</button>
  </div>
{:else if total === 0}
  {#if hasFilters}
    <p class="muted">
      No notes match these filters.
      <button class="linklike" onclick={clearFilters}>Clear all</button>
    </p>
  {:else}
    <div class="empty">
      <p class="empty__lead">No notes yet.</p>
      <p class="muted">
        Notes are your durable memory — the “why” behind decisions, shared with your AI agent.
        Create one with the <strong>＋</strong> in the sidebar, or run <code>nt mcp install</code> so an
        agent can capture them via <code>nt_note</code>.
      </p>
    </div>
  {/if}
{:else if layout === "timeline"}
  <div class="timeline">
    {#each timeline as month (month.key)}
      <h2 class="notegroup tl__month">{month.label}<span class="notegroup__count">{month.entries.length}</span></h2>
      <div class="tl__rail">
        {#each month.entries as e (e.card.handle)}
          <div class="tl__entry">
            <span class="tl__dot" aria-hidden="true"></span>
            <span class="tl__date">{e.date ? e.date.slice(5) : "—"}</span>
            <a class="tl__title" href={e.card.url}
              >{#if e.card.favorite}<span class="notecard__star" title="Favorite"><Icon name="star" filled size={13} /></span>{/if}{e.card.title}</a>
            {#if e.card.folder}<span class="noterow__folder">{e.card.folder}/</span>{/if}
            {#if e.card.tags && e.card.tags.length}
              <span class="noterow__tags">
                {#each e.card.tags.slice(0, 4) as t (t)}
                  <button class="chip" class:chip--active={activeTags.includes(t)} onclick={(ev) => toggleTag(t, ev)}>#{t}</button>
                {/each}
              </span>
            {/if}
          </div>
        {/each}
      </div>
    {/each}
  </div>
{:else}
  {#each groups as g (g.key)}
    {#if g.label}
      <h2 class="notegroup">{g.label}<span class="notegroup__count">{g.cards.length}</span></h2>
    {/if}
    {#if layout === "list"}
      <div class="notelist">
        {#each g.cards as n (n.handle)}
          <div class="noterow">
            <a class="noterow__title" href={n.url}
              >{#if n.favorite}<span class="notecard__star" title="Favorite"><Icon name="star" filled size={13} /></span>{/if}{n.title}</a>
            {#if n.folder}<span class="noterow__folder">{n.folder}/</span>{/if}
            {#if n.tags && n.tags.length}
              <span class="noterow__tags">
                {#each n.tags.slice(0, 5) as t (t)}
                  <button class="chip" class:chip--active={activeTags.includes(t)} onclick={(e) => toggleTag(t, e)}>#{t}</button>
                {/each}
              </span>
            {/if}
            {#if n.updated}<span class="noterow__date">{n.updated}</span>{/if}
          </div>
        {/each}
      </div>
    {:else}
      <div class="notegrid" class:notegrid--dense={layout === "compact"}>
        {#each g.cards as n (n.handle)}
          <div class="notecard">
            <div class="notecard__top">
              <a class="notecard__title notecard__link" href={n.url}
                >{#if n.favorite}<span class="notecard__star" title="Favorite"><Icon name="star" filled size={13} /></span>{/if}{n.title}</a>
              {#if n.updated}<span class="notecard__date">{n.updated}</span>{/if}
            </div>
            {#if n.folder}<span class="notecard__folder">{n.folder}/</span>{/if}
            {#if layout === "cards" && n.preview}<p class="notecard__preview">{n.preview}</p>{/if}
            {#if n.tags && n.tags.length}
              <div class="notecard__tags">
                {#each n.tags.slice(0, 4) as t (t)}
                  <button class="chip" class:chip--active={activeTags.includes(t)} onclick={(e) => toggleTag(t, e)}>#{t}</button>
                {/each}
                {#if n.tags.length > 4}<span class="chip chip--more" title={n.tags.join(" ")}>+{n.tags.length - 4}</span>{/if}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  {/each}
{/if}

<style>
  .notes-head {
    display: flex;
    align-items: center;
    gap: 16px;
  }
  /* Primary "New note" action + its inline title field, pushed to the right. */
  .newnote__open {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 5px 12px;
    background: var(--accent-fill);
    color: var(--on-accent);
    border: 1px solid var(--accent-fill);
    border-radius: var(--radius-sm);
    cursor: pointer;
    font-size: 0.85rem;
    font-weight: 600;
  }
  .newnote__open:hover {
    filter: brightness(1.06);
  }
  .newnote {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .newnote input {
    min-width: 220px;
    padding: 5px 10px;
    font-size: 0.85rem;
    background: var(--fill);
    border: 0.5px solid var(--separator);
    border-radius: var(--radius-sm);
    color: var(--fg);
  }
  .newnote input:focus {
    border-color: var(--accent);
  }
  .newnote__go {
    padding: 5px 12px;
    background: var(--accent-fill);
    color: var(--on-accent);
    border: 1px solid var(--accent-fill);
    border-radius: var(--radius-sm);
    cursor: pointer;
    font-size: 0.85rem;
  }
  .newnote__go:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .newnote__x {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: none;
    border: none;
    color: var(--muted);
    cursor: pointer;
    line-height: 1;
    padding: 4px 6px;
    border-radius: var(--radius-xs);
  }
  .newnote__x:hover {
    color: var(--fg);
  }
  .notes-controls {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .notes-filterrow {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    margin-top: 8px;
  }
  /* Quick text filter — mirrors the Tasks page's .qfilter look. */
  .qfilter {
    position: relative;
    flex: 1 1 220px;
    min-width: 200px;
  }
  .qfilter input {
    width: 100%;
    padding: 6px 28px 6px 12px;
    font-size: 0.9rem;
    background: var(--fill);
    border: 0.5px solid var(--separator);
    border-radius: var(--radius-sm);
    color: var(--fg);
  }
  .qfilter input:focus {
    border-color: var(--accent);
  }
  .qfilter__clear {
    position: absolute;
    right: 6px;
    top: 50%;
    transform: translateY(-50%);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: none;
    border: none;
    color: var(--muted);
    cursor: pointer;
    line-height: 1;
    padding: 2px 6px;
    border-radius: var(--radius-xs);
  }
  .qfilter__clear:hover {
    color: var(--fg);
  }
  .tagbar {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  .notes-clear {
    background: none;
    border: none;
    color: var(--accent);
    cursor: pointer;
    font-size: 0.8rem;
    padding: 4px 6px;
  }
  .notes-clear:hover {
    text-decoration: underline;
  }
  .linklike {
    background: none;
    border: none;
    color: var(--accent);
    cursor: pointer;
    font: inherit;
    padding: 0;
    text-decoration: underline;
  }
  /* macOS AppKit segmented control: a gray track holding an elevated pill on
     the selected segment (not a saturated accent fill). */
  .seg {
    display: flex;
    gap: 2px;
    padding: 2px;
    background: var(--fill);
    border: 0.5px solid var(--separator);
    border-radius: var(--radius-sm);
  }
  .seg button {
    padding: 4px 12px;
    background: transparent;
    border: none;
    border-radius: calc(var(--radius-sm) - 1px);
    color: var(--fg-soft);
    cursor: pointer;
    font-size: 0.8rem;
    transition:
      background var(--motion-fast) var(--ease),
      color var(--motion-fast) var(--ease),
      box-shadow var(--motion-fast) var(--ease);
  }
  .seg button.seg--on {
    background: var(--bg-elevated);
    color: var(--fg);
    box-shadow: var(--shadow-control);
  }
  /* On/off filter toggles: a tinted accent well when active (distinct from the
     neutral segmented track, which only ever switches one exclusive view). */
  .notes-toggle {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 5px 12px;
    background: var(--fill);
    border: 0.5px solid var(--separator);
    border-radius: var(--radius-sm);
    color: var(--fg-soft);
    cursor: pointer;
    font-size: 0.8rem;
  }
  .notes-toggle--on {
    background: var(--accent-fill);
    color: var(--on-accent);
    border-color: var(--accent-fill);
  }
  .notes-toggle__count {
    opacity: 0.8;
  }
  /* Sort key + direction toggle, joined into one pill. */
  .sortwrap {
    display: flex;
    align-items: stretch;
  }
  .sortwrap .select {
    border-top-right-radius: 0;
    border-bottom-right-radius: 0;
  }
  .dirbtn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: 0.5px solid var(--separator);
    border-left: none;
    border-top-right-radius: var(--radius-sm);
    border-bottom-right-radius: var(--radius-sm);
    background: var(--fill);
    color: var(--fg-soft);
    cursor: pointer;
    padding: 0 9px;
  }
  .dirbtn:hover {
    color: var(--fg);
  }
  /* Group headers. */
  .notegroup {
    display: flex;
    align-items: baseline;
    gap: 8px;
    margin: 20px 0 8px;
    font-size: 0.85rem;
    font-weight: 600;
    color: var(--fg-soft);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .notegroup__count {
    font-size: 0.75rem;
    font-weight: 400;
    color: var(--muted);
  }
  /* Interactive tag chips (the global .chip is borderless; reset button chrome). */
  .chip {
    border: none;
    font-family: inherit;
    cursor: pointer;
    line-height: 1.5;
  }
  button.chip:hover {
    background: var(--fill-strong);
  }
  .chip--active {
    background: var(--accent-fill);
    color: var(--on-accent);
  }
  .chip--more {
    cursor: default;
  }
  /* Card grid (unchanged from before, minus the anchor → div for stretched link). */
  .notegrid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
    gap: 12px;
    margin-top: 16px;
  }
  .notegrid--dense {
    grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
    gap: 8px;
  }
  .notecard {
    position: relative;
    display: block;
    padding: 12px 14px;
    border: 0.5px solid var(--separator);
    border-radius: var(--radius);
    background: var(--bg-elevated);
    color: var(--fg);
    box-shadow: var(--shadow-card);
    transition:
      border-color var(--motion-fast) var(--ease),
      box-shadow var(--motion-fast) var(--ease),
      transform var(--motion-fast) var(--ease);
  }
  .notecard:hover {
    border-color: var(--accent);
    transform: translateY(-1px);
  }
  .notegrid--dense .notecard {
    padding: 8px 10px;
  }
  .notecard__top {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
  }
  .notecard__title {
    font-weight: 600;
    color: var(--fg);
    text-decoration: none;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* Stretched link: the title anchor covers the whole card so the card is
     clickable, while tag buttons (z-indexed above) stay independently clickable. */
  .notecard__link::after {
    content: "";
    position: absolute;
    inset: 0;
  }
  .notecard__star {
    display: inline-flex;
    align-items: center;
    color: var(--pri-b);
    margin-right: 4px;
  }
  .notecard__date {
    flex: 0 0 auto;
    font-size: 0.7rem;
    color: var(--muted);
    font-family: var(--font-mono);
  }
  .notecard__folder {
    display: block;
    font-size: 0.72rem;
    color: var(--muted);
    font-family: var(--font-mono);
  }
  .notecard__preview {
    margin: 6px 0 0;
    font-size: 0.82rem;
    color: var(--fg-soft);
    line-height: 1.5;
    display: -webkit-box;
    -webkit-line-clamp: 3;
    line-clamp: 3;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .notecard__tags {
    position: relative;
    z-index: 1;
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-top: 8px;
  }
  /* List layout: dense one-line rows, scannable. */
  .notelist {
    margin-top: 14px;
    border-top: 0.5px solid var(--separator);
  }
  .noterow {
    position: relative;
    display: flex;
    align-items: baseline;
    gap: 10px;
    padding: 7px 10px;
    border-bottom: 0.5px solid var(--separator);
  }
  .noterow:hover {
    background: var(--fill);
  }
  .noterow__title {
    flex: 1 1 auto;
    font-weight: 600;
    color: var(--fg);
    text-decoration: none;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .noterow__title::after {
    content: "";
    position: absolute;
    inset: 0;
  }
  .noterow__folder {
    flex: 0 0 auto;
    font-size: 0.72rem;
    color: var(--muted);
    font-family: var(--font-mono);
  }
  .noterow__tags {
    position: relative;
    z-index: 1;
    display: flex;
    gap: 4px;
    flex: 0 0 auto;
    max-width: 40%;
    overflow: hidden;
  }
  .noterow__date {
    flex: 0 0 auto;
    font-size: 0.7rem;
    color: var(--muted);
    font-family: var(--font-mono);
  }
  /* Timeline layout: a vertical rail with month sections and dated entries. */
  .timeline {
    margin-top: 4px;
  }
  .tl__month {
    margin-top: 22px;
  }
  .tl__rail {
    position: relative;
    margin: 4px 0 0 8px;
    padding-left: 18px;
    border-left: 2px solid var(--separator-strong);
  }
  .tl__entry {
    position: relative;
    display: flex;
    align-items: baseline;
    gap: 10px;
    padding: 6px 8px;
    border-radius: var(--radius-sm);
  }
  .tl__entry:hover {
    background: var(--fill);
  }
  .tl__dot {
    position: absolute;
    left: -21px;
    top: 0.6em;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    box-shadow: 0 0 0 3px var(--bg);
  }
  .tl__date {
    flex: 0 0 auto;
    min-width: 3.4em;
    font-size: 0.7rem;
    color: var(--muted);
    font-family: var(--font-mono);
  }
  .tl__title {
    flex: 1 1 auto;
    font-weight: 600;
    color: var(--fg);
    text-decoration: none;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tl__title::after {
    content: "";
    position: absolute;
    inset: 0;
  }
</style>
