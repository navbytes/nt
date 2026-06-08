// Typed client for the Go JSON API (internal/web/api.go). These types mirror the
// Go DTOs; a later phase generates them from Go via tygo so the contract can't
// drift. For now they are hand-written to match api.go exactly.

export interface State {
  canEdit: boolean;
  csrf: string;
  version: string;
  openCount: number;
  noteCount: number;
  sources: string[];
}

export interface Task {
  id: string;
  text: string;
  status: string;
  due?: string;
  source?: string;
  project?: string;
  tags?: string[];
  blocker?: string;
}

export interface TaskGroup {
  status: string;
  tasks: Task[];
}

export interface TreeNode {
  name: string;
  path: string;
  url: string;
  isNote: boolean;
  children?: TreeNode[];
}

export interface NoteLink {
  url: string;
  title: string;
  path: string;
}

export interface NotesIndex {
  tree: TreeNode[];
  index: NoteLink[];
}

export interface Backlink {
  title: string;
  url: string;
  text: string;
  isNote: boolean;
}

export interface TaskRef {
  text: string;
  status: string;
  source: string;
}

export interface NoteView {
  id: string;
  title: string;
  folder: string;
  file: string;
  crumbs: string[];
  source: string;
  created: string;
  tags: string[];
  bodyHTML: string;
  backlinks: Backlink[];
  taskRefs: TaskRef[];
  prev?: NoteLink;
  next?: NoteLink;
  etag: string;
}

export interface ActivityEvent {
  when: string;
  action: string;
  kind: string;
  source: string;
  title: string;
  url?: string;
}

export interface ActivityDay {
  date: string;
  events: ActivityEvent[];
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
  tasks: () => getJSON<{ groups: TaskGroup[] }>("/api/tasks"),
  activity: (source = "") =>
    getJSON<{ days: ActivityDay[]; sources: string[] }>(
      "/api/activity" + (source ? `?source=${encodeURIComponent(source)}` : ""),
    ),
  search: (q: string, tag = "") =>
    getJSON<{ results: NoteLink[] }>(
      `/api/search?q=${encodeURIComponent(q)}` + (tag ? `&tag=${encodeURIComponent(tag)}` : ""),
    ),
  taskNew: (text: string) => postForm<{ groups: TaskGroup[] }>("/api/tasks", { text }),
  taskDone: (id: string) => postForm<{ groups: TaskGroup[] }>(`/api/tasks/${id}/done`),
  taskReopen: (id: string) => postForm<{ groups: TaskGroup[] }>(`/api/tasks/${id}/reopen`),
  taskStatus: (id: string, status: string) =>
    postForm<{ groups: TaskGroup[] }>(`/api/tasks/${id}/status`, { status }),
};
