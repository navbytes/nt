import { describe, it, expect } from "vitest";
import { taskMatcher } from "../lib/taskfilter";
import type { Task } from "../lib/api-types";

const T = (o: Partial<Task>): Task =>
  ({ id: "1", text: "", status: "open", ...o }) as Task;

describe("taskMatcher", () => {
  it("an empty/garbage filter matches everything", () => {
    expect(taskMatcher("").empty).toBe(true);
    expect(taskMatcher("   ").match(T({ text: "anything" }))).toBe(true);
  });
  it("ANDs bare words against the title, case-insensitively", () => {
    const m = taskMatcher("token race");
    expect(m.match(T({ text: "Fix token refresh race" }))).toBe(true);
    expect(m.match(T({ text: "token only" }))).toBe(false);
    expect(m.match(T({ text: "TOKEN RACE bug" }))).toBe(true);
  });
  it("filters by @tag (all must be present)", () => {
    const m = taskMatcher("@backend @api");
    expect(m.match(T({ text: "x", tags: ["backend", "api", "z"] }))).toBe(true);
    expect(m.match(T({ text: "x", tags: ["backend"] }))).toBe(false);
  });
  it("filters by +project and !priority", () => {
    expect(taskMatcher("+api").match(T({ text: "x", project: "api" }))).toBe(true);
    expect(taskMatcher("+api").match(T({ text: "x", project: "web" }))).toBe(false);
    expect(taskMatcher("!a").match(T({ text: "x", priority: "A" }))).toBe(true);
    expect(taskMatcher("!high").match(T({ text: "x", priority: "A" }))).toBe(true);
    expect(taskMatcher("!b").match(T({ text: "x", priority: "A" }))).toBe(false);
  });
  it("combines tokens (text AND tag AND project)", () => {
    const m = taskMatcher("fix @backend +api");
    expect(m.match(T({ text: "fix bug", tags: ["backend"], project: "api" }))).toBe(true);
    expect(m.match(T({ text: "fix bug", tags: ["backend"], project: "web" }))).toBe(false);
  });
});
