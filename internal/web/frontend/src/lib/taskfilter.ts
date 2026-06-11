import type { Task } from "./api-types";
import { parseQuickAdd } from "./quickparse";

// A client-side quick filter for the task list, using the same shorthand the
// quick-add box parses: bare words match the title (all must appear), @tags must
// all be present, +project must match, and a !priority (e.g. !a / !high) must
// match. Everything is ANDed and case-insensitive. An empty/garbage filter
// matches everything, so typing never hides the whole list mid-keystroke.

export interface TaskMatcher {
  /** True when nothing meaningful is being filtered (match-all). */
  empty: boolean;
  match: (t: Task) => boolean;
}

export function taskMatcher(query: string): TaskMatcher {
  const q = parseQuickAdd(query);
  const words = q.title.toLowerCase().split(/\s+/).filter(Boolean);
  const tags = q.tags.map((t) => t.toLowerCase());
  const project = q.project?.toLowerCase();
  const priority = q.priority; // already uppercased by parseQuickAdd

  const empty = words.length === 0 && tags.length === 0 && !project && !priority;
  if (empty) return { empty, match: () => true };

  return {
    empty,
    match(t: Task): boolean {
      const text = t.text.toLowerCase();
      if (!words.every((w) => text.includes(w))) return false;
      if (tags.length) {
        const tt = (t.tags ?? []).map((x) => x.toLowerCase());
        if (!tags.every((tag) => tt.includes(tag))) return false;
      }
      if (project && (t.project ?? "").toLowerCase() !== project) return false;
      if (priority && (t.priority ?? "").toUpperCase() !== priority) return false;
      return true;
    },
  };
}
