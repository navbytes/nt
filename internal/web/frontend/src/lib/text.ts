// displayTitle derives a short, scannable title from a task/note's full text —
// for places that can't wrap (graph node labels) or where a long line looks
// unpolished. Agents often capture a whole sentence as a task's only text
// (todo.txt has no title/body split), so we show the first clause and keep the
// full text one hover/expand away (never discarded).
//
// It prefers the first sentence/clause boundary (. ? ! — : ;), falling back to a
// hard cut at the last word boundary before max. The result never exceeds max+1
// (the ellipsis).
export function displayTitle(text: string, max = 60): string {
  const t = text.trim().replace(/\s+/g, " ");
  if (t.length <= max) return t;

  // First sentence/clause boundary within a sensible window.
  const window = t.slice(0, max + 1);
  const clause = window.search(/[.?!—:;](\s|$)/);
  if (clause >= 12) return t.slice(0, clause + 1).trim();

  // Otherwise cut at the last word boundary before max.
  const cut = window.lastIndexOf(" ");
  return (cut >= 12 ? t.slice(0, cut) : t.slice(0, max)).trimEnd() + "…";
}

// ---- task priority -------------------------------------------------------

// priorityClass maps a todo.txt priority letter to a small CSS modifier so the
// row can colour-code urgency (A=red, B=amber, C=blue, D–Z share a muted cue).
// Empty/invalid priorities get "" (no badge), so unprioritised tasks stay calm.
export function priorityClass(p?: string): "a" | "b" | "c" | "rest" | "" {
  if (!p) return "";
  const u = p.toUpperCase();
  if (u === "A") return "a";
  if (u === "B") return "b";
  if (u === "C") return "c";
  return u >= "D" && u <= "Z" ? "rest" : "";
}

// priorityRank turns a priority into a sortable number (A=0 … Z=25, none=99) so
// the most important work floats to the top of each bucket.
export function priorityRank(p?: string): number {
  if (!p) return 99;
  const c = p.toUpperCase().charCodeAt(0);
  return c >= 65 && c <= 90 ? c - 65 : 99;
}

// ---- due dates -----------------------------------------------------------

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

function startOfDay(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate());
}

// fmtTime renders a "HH:MM" 24h time as a terse 12h label: 17:00→"5pm",
// 17:30→"5:30pm", 09:05→"9:05am". Returns "" for anything unparseable.
export function fmtTime(hhmm: string): string {
  const m = /^(\d{1,2}):(\d{2})/.exec(hhmm);
  if (!m) return "";
  const h = parseInt(m[1]!, 10); // groups guaranteed present by the match above
  const min = parseInt(m[2]!, 10);
  if (h > 23 || min > 59) return "";
  const ap = h < 12 ? "am" : "pm";
  const h12 = h % 12 === 0 ? 12 : h % 12;
  return min === 0 ? `${h12}${ap}` : `${h12}:${String(min).padStart(2, "0")}${ap}`;
}

export interface DueInfo {
  /** Human label: "Today", "Tomorrow", "Sat", "3d ago", "Jun 20"… */
  label: string;
  /** Due date is strictly before today (date-only; a task due today is not yet overdue). */
  overdue: boolean;
  /** Due today or tomorrow — worth emphasising. */
  soon: boolean;
}

// relativeDue turns an ISO due value ("2026-06-11" or "2026-06-11T17:00") into a
// short, human-friendly label relative to `now`. Conventions mirror the apps
// people already know (Todoist/Things): Today/Tomorrow/Yesterday by name, the
// weekday for the coming week, "Nd ago" for the recent past, and an absolute
// "Mon D" (with year when it differs) for anything further out. A time-of-day is
// appended only for today/tomorrow, where it actually changes what you do next.
export function relativeDue(due: string, now: Date = new Date()): DueInfo {
  const datePart = due.slice(0, 10);
  const [y, mo, d] = datePart.split("-").map(Number);
  if (!y || !mo || !d) return { label: due, overdue: false, soon: false };

  const dueDay = new Date(y, mo - 1, d);
  const today = startOfDay(now);
  const diff = Math.round((dueDay.getTime() - today.getTime()) / 86400000);
  const time = due.includes("T") ? fmtTime(due.slice(11, 16)) : "";

  let label: string;
  if (diff === 0) label = time ? `Today ${time}` : "Today";
  else if (diff === 1) label = time ? `Tomorrow ${time}` : "Tomorrow";
  else if (diff === -1) label = "Yesterday";
  else if (diff >= 2 && diff <= 6) label = WEEKDAYS[dueDay.getDay()]!; // getDay() ∈ 0..6
  else if (diff <= -2 && diff >= -6) label = `${-diff}d ago`;
  else label = `${MONTHS[mo - 1]} ${d}${y !== now.getFullYear() ? ` ${y}` : ""}`;

  return { label, overdue: diff < 0, soon: diff >= 0 && diff <= 1 };
}

// ---- task source ---------------------------------------------------------

// Default origins (you, at a keyboard) are noise on every row. We only surface a
// source badge when it's something worth knowing — chiefly an AI agent that
// captured the task — which is exactly nt's reason to exist.
const QUIET_SOURCES = new Set(["", "cli", "web", "tui", "user"]);

export function meaningfulSource(s?: string): string {
  if (!s) return "";
  return QUIET_SOURCES.has(s.toLowerCase()) ? "" : s;
}
