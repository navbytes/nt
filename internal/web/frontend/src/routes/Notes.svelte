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

  // "Fresh" = touched within the last 3 days. Drives a subtle spectral pulse on
  // the card/row so recently-edited notes feel alive (purely a visual cue).
  const freshUrls = $derived.by(() => {
    const c = cutoff(3);
    const s = new Set<string>();
    for (const n of $gridQ.data?.notes ?? []) if ((n.updated ?? "") >= c) s.add(n.url);
    return s;
  });

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

<div class="pagehead notes-pagehead">
  <div class="notes-head">
    <div class="notes-head__titles">
      <p class="notes-eyebrow">Knowledge</p>
      <h1>Notes</h1>
    </div>
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
          <button type="submit" class="btn btn--sm newnote__go" disabled={creating || !newTitle.trim()}>Create</button>
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
        <button class="btn newnote__open" onclick={openNew}><Icon name="plus" size={14} /> New note</button>
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
        ><Icon name="star" filled={favoritesOnly} size={14} /> Favorites{#if favoriteCount}<span class="notes-toggle__count">{favoriteCount}</span>{/if}</button>
      {/if}
      <button
        class="notes-toggle"
        class:notes-toggle--on={orphansOnly}
        aria-pressed={orphansOnly}
        title="Show only notes with no links in or out"
        onclick={() => (orphansOnly = !orphansOnly)}
      >Orphans{#if $orphansQ.data?.notes.length}<span class="notes-toggle__count">{$orphansQ.data.notes.length}</span>{/if}</button>
      {#if archivedCount > 0 || archivedOnly}
        <button
          class="notes-toggle"
          class:notes-toggle--on={archivedOnly}
          aria-pressed={archivedOnly}
          title="Show retired notes (hidden from the sidebar, search, and graph)"
          onclick={() => (archivedOnly = !archivedOnly)}
        ><Icon name="archive" size={14} /> Archived{#if archivedCount}<span class="notes-toggle__count">{archivedCount}</span>{/if}</button>
      {/if}
    </div>

    <div class="notes-filterrow">
      <div class="qfilter">
        <Icon name="search" size={15} />
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
    <div class="empty empty--hero">
      <span class="empty__art empty__art--onboard"><Icon name="document" size={28} /></span>
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
          <div class="tl__entry" class:tl__entry--fresh={freshUrls.has(e.card.url)} class:tl__entry--orphan={orphanUrls.has(e.card.url)} class:tl__entry--fav={e.card.favorite}>
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
          <div class="noterow" class:noterow--fresh={freshUrls.has(n.url)} class:noterow--orphan={orphanUrls.has(n.url)} class:noterow--fav={n.favorite}>
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
          <div
            class="notecard"
            class:notecard--fav={n.favorite}
            class:notecard--fresh={freshUrls.has(n.url)}
            class:notecard--orphan={orphanUrls.has(n.url)}
          >
            {#if n.favorite}<span class="notecard__edge" aria-hidden="true"></span>{/if}
            <div class="notecard__top">
              <a class="notecard__title notecard__link" href={n.url}
                >{#if n.favorite}<span class="notecard__star" title="Favorite"><Icon name="star" filled size={13} /></span>{/if}{n.title}</a>
              {#if freshUrls.has(n.url) && !n.archived}<span class="notecard__fresh" title="Recently edited" aria-hidden="true"></span>{/if}
            </div>
            <div class="notecard__meta">
              {#if n.folder}<span class="notecard__folder">{n.folder}/</span>{/if}
              {#if n.updated}<span class="notecard__date">{n.updated}</span>{/if}
            </div>
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
  /* The pagehead is a column: title row, control row, filter row. */
  .notes-pagehead {
    display: block;
  }
  .notes-head {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    margin-bottom: var(--space-5);
  }
  .notes-head__titles {
    display: flex;
    flex-direction: column;
  }
  /* Mono eyebrow above the serif title — the Home-hero / sidebar language. */
  .notes-eyebrow {
    margin: 0 0 2px;
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    letter-spacing: var(--tracking-caps);
    text-transform: uppercase;
    color: var(--accent-color);
  }
  .notes-head h1 {
    margin: 0;
  }
  /* "New note" CTA + inline title field, pushed to the right. The button reuses
     the global .btn (spectral gradient + glow); only layout is set here. */
  .newnote__open {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .newnote {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .newnote input {
    min-width: 220px;
    padding: 6px 11px;
    font: inherit;
    font-size: var(--text-body);
    background: var(--fill);
    border: 0.5px solid var(--separator-strong);
    border-radius: var(--radius-sm);
    color: var(--fg);
    transition:
      border-color var(--motion-fast) var(--ease),
      box-shadow var(--motion-fast) var(--ease);
  }
  .newnote input:focus {
    outline: none;
    border-color: var(--accent-color);
    box-shadow: var(--focus-ring-tight);
  }
  .newnote__go {
    flex: none;
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
    padding: 5px 6px;
    border-radius: var(--radius-xs);
    transition: color var(--motion-fast) var(--ease);
  }
  .newnote__x:hover {
    color: var(--fg);
  }
  .notes-controls {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex-wrap: wrap;
  }
  /* Selects: glassy, hairline-bordered controls that sit beside the segmented
     control without fighting it. */
  .notes-controls :global(.select) {
    background: color-mix(in srgb, var(--bg-elevated) 70%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    border: 0.5px solid var(--separator);
    box-shadow: var(--glass-hairline);
    color: var(--label-secondary);
    transition:
      border-color var(--motion-fast) var(--ease),
      color var(--motion-fast) var(--ease);
  }
  .notes-controls :global(.select:hover) {
    color: var(--fg);
    border-color: var(--separator-strong);
  }
  .notes-filterrow {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex-wrap: wrap;
    margin-top: var(--space-3);
  }
  /* Quick text filter — a glass field with a leading search glyph. */
  .qfilter {
    position: relative;
    display: flex;
    align-items: center;
    flex: 1 1 240px;
    min-width: 200px;
  }
  .qfilter :global(.icon) {
    position: absolute;
    left: 10px;
    color: var(--muted);
    pointer-events: none;
  }
  .qfilter input {
    width: 100%;
    padding: 7px 28px 7px 32px;
    font: inherit;
    font-size: var(--text-body);
    background: color-mix(in srgb, var(--bg-elevated) 70%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    border: 0.5px solid var(--separator);
    box-shadow: var(--glass-hairline);
    border-radius: var(--radius-sm);
    color: var(--fg);
    transition:
      border-color var(--motion-fast) var(--ease),
      box-shadow var(--motion-fast) var(--ease);
  }
  .qfilter input:focus {
    outline: none;
    border-color: var(--accent-color);
    box-shadow: var(--focus-ring-tight);
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
    transition: color var(--motion-fast) var(--ease);
  }
  .qfilter__clear:hover {
    color: var(--fg);
  }
  .tagbar {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }
  .notes-clear {
    background: none;
    border: none;
    color: var(--accent-color);
    cursor: pointer;
    font-family: var(--font-mono);
    font-size: var(--text-callout);
    padding: 4px 6px;
  }
  .notes-clear:hover {
    text-decoration: underline;
  }
  .linklike {
    background: none;
    border: none;
    color: var(--accent-color);
    cursor: pointer;
    font: inherit;
    padding: 0;
    text-decoration: underline;
  }
  /* Glass segmented control: a translucent track holding an elevated pill on the
     selected segment, with a hairline spectral underline. Mono microlabels —
     the shipped sibling language (Tasks/Review). No gradient under label text. */
  .seg {
    display: flex;
    gap: 2px;
    padding: 3px;
    background: color-mix(in srgb, var(--bg-elevated) 70%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    border-radius: var(--radius-sm);
    box-shadow: var(--glass-hairline), 0 0 0 0.5px var(--separator);
  }
  .seg button {
    position: relative;
    padding: 4px 13px;
    background: transparent;
    border: none;
    border-radius: calc(var(--radius-sm) - 1px);
    color: var(--label-secondary);
    cursor: pointer;
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    transition:
      background var(--motion-fast) var(--ease),
      color var(--motion-fast) var(--ease),
      box-shadow var(--motion-fast) var(--ease);
  }
  .seg button:hover:not(.seg--on) {
    color: var(--fg);
    background: var(--fill-hover);
  }
  .seg--on {
    background: var(--bg-elevated);
    color: var(--fg);
    box-shadow: var(--shadow-control);
  }
  /* The short spectral underline anchoring the active segment (decorative). */
  .seg--on::after {
    content: "";
    position: absolute;
    left: 50%;
    bottom: 2px;
    transform: translateX(-50%);
    width: 16px;
    height: 2px;
    border-radius: 2px;
    background: var(--grad-spectral);
  }
  /* On/off filter toggles: a tinted accent well when active (distinct from the
     neutral segmented track, which only ever switches one exclusive view). */
  .notes-toggle {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 5px 11px;
    background: color-mix(in srgb, var(--bg-elevated) 70%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    border: 0.5px solid var(--separator);
    box-shadow: var(--glass-hairline);
    border-radius: var(--radius-sm);
    color: var(--label-secondary);
    cursor: pointer;
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
    transition:
      background var(--motion-fast) var(--ease),
      color var(--motion-fast) var(--ease),
      border-color var(--motion-fast) var(--ease);
  }
  .notes-toggle:hover {
    color: var(--fg);
    border-color: var(--separator-strong);
  }
  .notes-toggle--on,
  .notes-toggle--on:hover {
    background: var(--accent-tint);
    color: var(--accent-color);
    border-color: color-mix(in srgb, var(--accent-color) 40%, transparent);
  }
  .notes-toggle__count {
    font-variant-numeric: tabular-nums;
    opacity: 0.85;
  }
  /* Sort key + direction toggle, joined into one pill. */
  .sortwrap {
    display: flex;
    align-items: stretch;
  }
  .sortwrap :global(.select) {
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
    background: color-mix(in srgb, var(--bg-elevated) 70%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    color: var(--label-secondary);
    cursor: pointer;
    padding: 0 9px;
    transition: color var(--motion-fast) var(--ease);
  }
  .dirbtn:hover {
    color: var(--fg);
  }
  /* Group headers — mono uppercase microlabel + a count. */
  .notegroup {
    display: flex;
    align-items: baseline;
    gap: var(--space-3);
    margin: var(--space-7) 0 var(--space-3);
    font-family: var(--font-mono);
    font-size: var(--text-subhead);
    font-weight: 500;
    color: var(--label-secondary);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
  }
  .notegroup__count {
    font-size: var(--text-subhead);
    font-weight: 400;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }
  /* Interactive tag chips (the global .chip is borderless; reset button chrome). */
  .chip {
    border: none;
    font-family: var(--font-mono);
    cursor: pointer;
    line-height: 1.5;
  }
  button.chip:hover {
    background: var(--fill-strong);
  }
  .chip--active {
    background: var(--accent-tint);
    color: var(--accent-color);
    box-shadow: inset 0 0 0 0.5px color-mix(in srgb, var(--accent-color) 40%, transparent);
  }
  .chip--more {
    cursor: default;
  }

  /* ── Card grid (glass bento) ─────────────────────────────────────────────
     Cards are translucent glass panels with the bento depth shadow + radius-lg.
     A keyed entrance cascade fades them up on load (reduced-motion safe). */
  .notegrid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(248px, 1fr));
    gap: var(--space-4);
    margin-top: var(--space-5);
  }
  .notegrid--dense {
    grid-template-columns: repeat(auto-fill, minmax(184px, 1fr));
    gap: var(--space-3);
  }
  .notecard {
    position: relative;
    display: block;
    padding: var(--space-5);
    border-radius: var(--radius-lg);
    background: color-mix(in srgb, var(--bg-elevated) 82%, transparent);
    -webkit-backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    backdrop-filter: saturate(var(--glass-saturate)) blur(var(--glass-blur));
    color: var(--fg);
    box-shadow: var(--shadow-bento);
    overflow: hidden;
    animation: row-in var(--motion) var(--ease-out) both;
    transition:
      box-shadow var(--motion) var(--ease),
      transform var(--motion) var(--ease);
  }
  /* Stagger the first few cards; later ones share the last delay (matches the
     shipped .rows cascade). Keyed {#each} means this fires once, not on refetch. */
  .notegrid .notecard:nth-child(2) {
    animation-delay: 28ms;
  }
  .notegrid .notecard:nth-child(3) {
    animation-delay: 56ms;
  }
  .notegrid .notecard:nth-child(4) {
    animation-delay: 84ms;
  }
  .notegrid .notecard:nth-child(5) {
    animation-delay: 112ms;
  }
  .notegrid .notecard:nth-child(n + 6) {
    animation-delay: 140ms;
  }
  .notecard:hover {
    transform: translateY(-2px);
    box-shadow: var(--shadow-float);
  }
  .notegrid--dense .notecard {
    padding: var(--space-4);
    border-radius: var(--radius-md);
  }
  /* Starred/pinned: a spectral edge ribbon down the left + a faint glow. */
  .notecard--fav {
    box-shadow: var(--shadow-bento), var(--glow-spectral);
  }
  .notecard__edge {
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    width: 3px;
    background: var(--grad-spectral-160);
  }
  /* Orphan (no links in or out): gently dimmed — present but quieter. */
  .notecard--orphan {
    opacity: 0.62;
  }
  .notecard--orphan:hover {
    opacity: 1;
  }
  .notecard__top {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-3);
  }
  /* Serif card title — editorial, confident; clamps to two lines. */
  .notecard__title {
    font-family: var(--font-serif);
    font-size: var(--text-title2);
    font-weight: 600;
    letter-spacing: -0.01em;
    line-height: var(--leading-tight);
    color: var(--label-primary);
    text-decoration: none;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .notegrid--dense .notecard__title {
    font-size: var(--text-body);
    -webkit-line-clamp: 1;
    line-clamp: 1;
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
    margin-right: 5px;
    vertical-align: -0.1em;
  }
  /* Fresh dot — a spectral pip that pulses for recently-edited notes. */
  .notecard__fresh {
    flex: none;
    margin-top: 5px;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--grad-spectral);
    box-shadow: 0 0 0 0 var(--spectral-glow);
    animation: fresh-pulse 2.6s var(--ease) infinite;
  }
  @keyframes fresh-pulse {
    0%,
    100% {
      box-shadow: 0 0 0 0 color-mix(in srgb, var(--spectral-2) 50%, transparent);
    }
    50% {
      box-shadow: 0 0 0 4px color-mix(in srgb, var(--spectral-2) 0%, transparent);
    }
  }
  .notecard__meta {
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: var(--space-3);
    margin-top: var(--space-2);
  }
  .notecard__date {
    flex: 0 0 auto;
    font-size: var(--text-subhead);
    color: var(--muted);
    font-family: var(--font-mono);
    letter-spacing: var(--tracking-caps);
    font-variant-numeric: tabular-nums;
  }
  .notecard__folder {
    font-size: var(--text-subhead);
    color: var(--muted);
    font-family: var(--font-mono);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
  }
  .notecard__preview {
    margin: var(--space-3) 0 0;
    font-size: var(--text-callout);
    color: var(--label-secondary);
    line-height: 1.55;
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
    gap: var(--space-2);
    margin-top: var(--space-3);
  }

  /* ── List layout: dense one-line rows, scannable ─────────────────────────── */
  .notelist {
    margin-top: var(--space-4);
    border-top: 0.5px solid var(--separator);
  }
  .noterow {
    position: relative;
    display: flex;
    align-items: baseline;
    gap: var(--space-4);
    padding: 8px 10px;
    border-bottom: 0.5px solid var(--separator);
    transition: background var(--motion-fast) var(--ease);
  }
  .noterow::before {
    content: "";
    position: absolute;
    left: 0;
    top: 6px;
    bottom: 6px;
    width: 2px;
    border-radius: 2px;
    background: transparent;
  }
  .noterow--fav::before {
    background: var(--grad-spectral-160);
  }
  .noterow:hover {
    background: var(--fill-hover);
  }
  .noterow--orphan {
    opacity: 0.6;
  }
  .noterow--orphan:hover {
    opacity: 1;
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
  .noterow--fresh .noterow__title {
    color: var(--label-primary);
  }
  .noterow__title::after {
    content: "";
    position: absolute;
    inset: 0;
  }
  .noterow__folder {
    flex: 0 0 auto;
    font-size: var(--text-subhead);
    color: var(--muted);
    font-family: var(--font-mono);
    text-transform: uppercase;
    letter-spacing: var(--tracking-caps);
  }
  .noterow__tags {
    position: relative;
    z-index: 1;
    display: flex;
    gap: var(--space-2);
    flex: 0 0 auto;
    max-width: 40%;
    overflow: hidden;
  }
  .noterow__date {
    flex: 0 0 auto;
    font-size: var(--text-subhead);
    color: var(--muted);
    font-family: var(--font-mono);
    letter-spacing: var(--tracking-caps);
    font-variant-numeric: tabular-nums;
  }

  /* ── Timeline: a vertical rail with month sections + spectral dots ───────── */
  .timeline {
    margin-top: var(--space-2);
  }
  .tl__month {
    margin-top: var(--space-7);
  }
  /* The spine is a spectral gradient thread (a positioned bar, so no gradient
     ever sits behind the entry text). */
  .tl__rail {
    position: relative;
    margin: var(--space-2) 0 0 8px;
    padding-left: 20px;
  }
  .tl__rail::before {
    content: "";
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    width: 2px;
    border-radius: 2px;
    background: var(--grad-spectral-160);
    opacity: 0.55;
  }
  .tl__entry {
    position: relative;
    display: flex;
    align-items: baseline;
    gap: var(--space-3);
    padding: 6px 8px;
    border-radius: var(--radius-sm);
    transition: background var(--motion-fast) var(--ease);
  }
  .tl__entry:hover {
    background: var(--fill-hover);
  }
  .tl__entry--orphan {
    opacity: 0.6;
  }
  .tl__entry--orphan:hover {
    opacity: 1;
  }
  /* Dated dot — spectral gradient, ringed by the page bg so it punches the rail. */
  .tl__dot {
    position: absolute;
    left: -23px;
    top: 0.55em;
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--grad-spectral);
    box-shadow: 0 0 0 3px var(--bg-content);
  }
  .tl__entry--fresh .tl__dot {
    animation: fresh-pulse 2.6s var(--ease) infinite;
  }
  .tl__entry--fav .tl__dot {
    box-shadow:
      0 0 0 3px var(--bg-content),
      var(--glow-spectral);
  }
  .tl__date {
    flex: 0 0 auto;
    min-width: 3.4em;
    font-size: var(--text-subhead);
    color: var(--muted);
    font-family: var(--font-mono);
    letter-spacing: var(--tracking-caps);
    font-variant-numeric: tabular-nums;
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

  /* The fresh-pulse glow is ambient/infinite; silence it under reduced-motion
     (the global rule covers transitions, but belt-and-braces for the loop). */
  @media (prefers-reduced-motion: reduce) {
    .notecard__fresh,
    .tl__entry--fresh .tl__dot {
      animation: none;
    }
  }

  /* Narrow viewports: stack the title row so the New-note control wraps cleanly. */
  @media (max-width: 640px) {
    .notes-head {
      flex-wrap: wrap;
    }
    .newnote,
    .newnote__open {
      margin-left: 0;
    }
    .noterow__tags {
      display: none;
    }
  }
</style>
