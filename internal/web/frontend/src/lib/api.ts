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
  if (!r.ok) throw new Error(`${path} → ${r.status}`);
  return (await r.json()) as T;
}

export const api = {
  state: () => getJSON<State>("/api/state"),
  notes: () => getJSON<NotesIndex>("/api/notes"),
  tasks: () => getJSON<{ groups: TaskGroup[] }>("/api/tasks"),
  taskDone: (id: string) => postForm<{ groups: TaskGroup[] }>(`/api/tasks/${id}/done`),
  taskNew: (text: string) => postForm<{ groups: TaskGroup[] }>("/api/tasks", { text }),
};
