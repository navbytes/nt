import { describe, it, expect } from "vitest";
import { dayCapacity } from "../lib/capacity";
import type { TaskGroup } from "../lib/api-types";

const g = (status: string, tasks: { est?: number; status?: string }[]): TaskGroup =>
  ({ status, tasks: tasks.map((t, i) => ({ id: String(i), text: "t", status: t.status ?? "open", est: t.est })) }) as TaskGroup;

describe("dayCapacity", () => {
  it("sums est minutes against the budget and reports the fraction", () => {
    const c = dayCapacity([g("Overdue", [{ est: 90 }]), g("Today", [{ est: 60 }, { est: 30 }])], 360);
    expect(c.plannedMin).toBe(180);
    expect(c.unestimated).toBe(0);
    expect(c.fraction).toBeCloseTo(0.5);
    expect(c.over).toBe(false);
    expect(c.label).toBe("3h planned of 6h");
  });
  it("counts unestimated tasks separately and notes them", () => {
    const c = dayCapacity([g("Today", [{ est: 60 }, {}, {}])], 120);
    expect(c.plannedMin).toBe(60);
    expect(c.unestimated).toBe(2);
    expect(c.label).toContain("2 no estimate");
  });
  it("flags over-budget and clamps the bar fraction to 1", () => {
    const c = dayCapacity([g("Today", [{ est: 480 }])], 360);
    expect(c.over).toBe(true);
    expect(c.fraction).toBe(1);
  });
  it("ignores done tasks and the Done bucket", () => {
    const c = dayCapacity([g("Today", [{ est: 60, status: "done" }]), g("Done", [{ est: 999 }])], 360);
    expect(c.plannedMin).toBe(0);
    expect(c.unestimated).toBe(0);
  });
});
