import type { Task, TaskGroup } from "./api-types";
import { fmtDuration } from "./text";

export interface Capacity {
  /** Sum of est: minutes across the planned (overdue + due-today, not done) tasks. */
  plannedMin: number;
  /** How many of those tasks carry no est: (so the total is a floor, not exact). */
  unestimated: number;
  /** The day's budget in minutes. */
  budgetMin: number;
  /** plannedMin / budgetMin, clamped to [0, 1] for the bar width. */
  fraction: number;
  /** Planned exceeds budget. */
  over: boolean;
  /** Human summary, e.g. "3h 30m planned of 6h" (+ "· N no estimate"). */
  label: string;
}

// dayCapacity sums the time estimates of today's plan (overdue + due-today, open)
// against a daily budget — the "plan my day" signal calm task apps surface so you
// can tell at a glance whether you've over-committed. Tasks without an est: are
// counted separately (the total is a floor), never guessed at.
export function dayCapacity(groups: TaskGroup[], budgetMin: number): Capacity {
  let plannedMin = 0;
  let unestimated = 0;
  for (const g of groups) {
    if (g.status === "Done" || g.status === "done") continue;
    for (const t of g.tasks as Task[]) {
      if (t.status === "done") continue;
      if (t.est && t.est > 0) plannedMin += t.est;
      else unestimated++;
    }
  }
  const fraction = budgetMin > 0 ? Math.min(plannedMin / budgetMin, 1) : 0;
  let label = `${fmtDuration(plannedMin)} planned of ${fmtDuration(budgetMin)}`;
  if (unestimated > 0) label += ` · ${unestimated} no estimate`;
  return { plannedMin, unestimated, budgetMin, fraction, over: plannedMin > budgetMin, label };
}
