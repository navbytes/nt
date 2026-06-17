// Typed client for the Go JSON API (internal/web/api.go). The wire types are
// generated from the Go apitypes package by tygo (api-types.ts) and re-exported
// here, so a contract change in Go surfaces as a TS error. This file adds only
// the runtime concerns: CSRF, fetch helpers, and the typed endpoint map.
import type {
  State,
  NotesIndex,
  NotesGrid,
  NoteView,
  RawNote,
  TasksResponse,
  ViewsResponse,
  ReviewResponse,
  ActivityResponse,
  SearchResponse,
  TagsResponse,
  OrphansResponse,
  GraphData,
  CreatedNote,
  MovedNote,
  ArchivedNote,
  FavoritedNote,
  DeletedNote,
  NoteTags,
  JournalResponse,
} from "./api-types";

export type * from "./api-types";

/** Thrown by api.save when the note changed on disk since it was opened (HTTP 409). */
export class SaveConflict extends Error {
  constructor() {
    super("note changed on disk since you opened it");
    this.name = "SaveConflict";
  }
}

let csrf = "";
export function setCsrf(token: string): void {
  csrf = token;
}

async function getJSON<T>(path: string): Promise<T> {
  const r = await fetch(path, { headers: { Accept: "application/json" } });
  if (!r.ok) throw new Error(`${path} → ${r.status}`);
  return (await r.json()) as T;
}

async function postForm<T>(path: string, body?: Record<string, string>): Promise<T> {
  const r = await fetch(path, {
    method: "POST",
    headers: {
      "X-CSRF": csrf,
      "Content-Type": "application/x-www-form-urlencoded",
    },
    body: body ? new URLSearchParams(body).toString() : undefined,
  });
  if (!r.ok) throw new Error(`${path} → ${(await r.text()) || r.status}`);
  return (await r.json()) as T;
}

export const api = {
  state: () => getJSON<State>("/api/state"),
  notes: () => getJSON<NotesIndex>("/api/notes"),
  notesGrid: () => getJSON<NotesGrid>("/api/notes/grid"),
  note: (handle: string) => getJSON<NoteView>(`/api/notes/${encodeURIComponent(handle)}`),
  tasks: () => getJSON<TasksResponse>("/api/tasks"),
  /** Apply a saved smart view (nt view) server-side; one group, in view order. */
  tasksView: (name: string) => getJSON<TasksResponse>(`/api/tasks?view=${encodeURIComponent(name)}`),
  views: () => getJSON<ViewsResponse>("/api/views"),
  review: () => getJSON<ReviewResponse>("/api/review"),
  activity: (source = "") =>
    getJSON<ActivityResponse>(
      "/api/activity" + (source ? `?source=${encodeURIComponent(source)}` : ""),
    ),
  search: (q: string, tag = "") =>
    getJSON<SearchResponse>(
      `/api/search?q=${encodeURIComponent(q)}` + (tag ? `&tag=${encodeURIComponent(tag)}` : ""),
    ),
  tags: () => getJSON<TagsResponse>("/api/tags"),
  orphans: () => getJSON<OrphansResponse>("/api/orphans"),
  graph: () => getJSON<GraphData>("/api/graph"),
  journal: () => getJSON<JournalResponse>("/api/journal"),
  taskNew: (text: string) => postForm<TasksResponse>("/api/tasks", { text }),
  taskEdit: (id: string, text: string) => postForm<TasksResponse>(`/api/tasks/${id}`, { text }),
  /** Reschedule only: due accepts the quick-add NL forms ("today", "fri 5pm",
   *  "+7d"); "none" clears. Resolved by the server's dateparse — no client drift. */
  taskDue: (id: string, due: string) => postForm<TasksResponse>(`/api/tasks/${id}`, { due }),
  taskDone: (id: string) => postForm<TasksResponse>(`/api/tasks/${id}/done`),
  taskReopen: (id: string) => postForm<TasksResponse>(`/api/tasks/${id}/reopen`),
  taskStatus: (id: string, status: string) =>
    postForm<TasksResponse>(`/api/tasks/${id}/status`, { status }),
  /** Apply one action to many tasks in a SINGLE transaction (so one undo reverts
   *  the whole batch). action: "done" | "delete" | "due"; due takes NL forms. */
  taskBulk: (action: "done" | "delete" | "due", ids: string[], due = "") =>
    postForm<TasksResponse>("/api/tasks/bulk", { action, ids: ids.join(","), ...(action === "due" ? { due } : {}) }),
  /** Give a task a "body": create (or return its existing) linked detail note.
   *  Returns the note's handle + URL to open in the editor. */
  taskNote: (id: string) => postForm<CreatedNote>(`/api/tasks/${id}/note`),
  /** Revert the latest task write (the toast's Undo). 409 = nothing to undo or
   *  another writer changed the touched tasks (the engine refuses, never corrupts). */
  undo: () => postForm<TasksResponse>("/api/undo"),
  taskDelete: async (id: string): Promise<TasksResponse> => {
    const r = await fetch(`/api/tasks/${id}`, { method: "DELETE", headers: { "X-CSRF": csrf } });
    if (!r.ok) throw new Error(`${(await r.text()) || r.status}`);
    return (await r.json()) as TasksResponse;
  },

  noteCreate: (title: string, folder = "") =>
    postForm<CreatedNote>("/api/notes", folder ? { title, folder } : { title }),

  noteMove: (handle: string, folder: string) =>
    postForm<MovedNote>(`/api/notes/${encodeURIComponent(handle)}/move`, { folder }),

  noteArchive: (handle: string, archived: boolean) =>
    postForm<ArchivedNote>(`/api/notes/${encodeURIComponent(handle)}/archive`, {
      archived: String(archived),
    }),

  noteFavorite: (handle: string, favorite: boolean) =>
    postForm<FavoritedNote>(`/api/notes/${encodeURIComponent(handle)}/favorite`, {
      favorite: String(favorite),
    }),

  /** Delete a note to .trash/. mode "" refuses (409) when the note has inbound
   *  [[links]]; "unlink" strips them first, "force" deletes anyway (dangling). */
  noteDelete: async (handle: string, mode: "" | "unlink" | "force" = ""): Promise<DeletedNote> => {
    const q = mode ? `?mode=${mode}` : "";
    const r = await fetch(`/api/notes/${encodeURIComponent(handle)}${q}`, {
      method: "DELETE",
      headers: { "X-CSRF": csrf },
    });
    if (!r.ok) throw new Error(`${(await r.text()) || r.status}`);
    return (await r.json()) as DeletedNote;
  },

  /** Edit a note's frontmatter tags only (body untouched). add/remove are
   *  comma/space-separated; returns the tags after the edit. */
  noteTags: (handle: string, add: string, remove = "") =>
    postForm<NoteTags>(`/api/notes/${encodeURIComponent(handle)}/tags`, { add, remove }),

  // ---- editor ----
  raw: (handle: string) => getJSON<RawNote>(`/api/notes/${encodeURIComponent(handle)}/raw`),

  preview: async (text: string, signal?: AbortSignal): Promise<string> => {
    const r = await fetch("/api/preview", {
      method: "POST",
      headers: { "X-CSRF": csrf },
      body: text,
      signal,
    });
    if (!r.ok) throw new Error(`preview → ${r.status}`);
    return r.text();
  },

  save: async (handle: string, text: string, etag: string): Promise<void> => {
    const r = await fetch(`/api/notes/${encodeURIComponent(handle)}`, {
      method: "POST",
      headers: { "X-CSRF": csrf, "If-Match": etag },
      body: text,
    });
    if (r.status === 409) throw new SaveConflict();
    if (!r.ok) throw new Error(`${(await r.text()) || r.status}`);
  },
};
