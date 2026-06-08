// Typed client for the Go JSON API (internal/web/api.go). The wire types are
// generated from the Go apitypes package by tygo (api-types.ts) and re-exported
// here, so a contract change in Go surfaces as a TS error. This file adds only
// the runtime concerns: CSRF, fetch helpers, and the typed endpoint map.
import type {
  State,
  NotesIndex,
  NoteView,
  RawNote,
  TasksResponse,
  ActivityResponse,
  SearchResponse,
  TagsResponse,
  OrphansResponse,
  GraphData,
  CreatedNote,
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
  note: (handle: string) => getJSON<NoteView>(`/api/notes/${encodeURIComponent(handle)}`),
  tasks: () => getJSON<TasksResponse>("/api/tasks"),
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
  taskNew: (text: string) => postForm<TasksResponse>("/api/tasks", { text }),
  taskDone: (id: string) => postForm<TasksResponse>(`/api/tasks/${id}/done`),
  taskReopen: (id: string) => postForm<TasksResponse>(`/api/tasks/${id}/reopen`),
  taskStatus: (id: string, status: string) =>
    postForm<TasksResponse>(`/api/tasks/${id}/status`, { status }),
  taskDelete: async (id: string): Promise<TasksResponse> => {
    const r = await fetch(`/api/tasks/${id}`, { method: "DELETE", headers: { "X-CSRF": csrf } });
    if (!r.ok) throw new Error(`${(await r.text()) || r.status}`);
    return (await r.json()) as TasksResponse;
  },

  noteCreate: (title: string, folder = "") =>
    postForm<CreatedNote>("/api/notes", folder ? { title, folder } : { title }),

  // ---- editor ----
  raw: (handle: string) => getJSON<RawNote>(`/api/notes/${encodeURIComponent(handle)}/raw`),

  preview: async (text: string): Promise<string> => {
    const r = await fetch("/api/preview", { method: "POST", headers: { "X-CSRF": csrf }, body: text });
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
