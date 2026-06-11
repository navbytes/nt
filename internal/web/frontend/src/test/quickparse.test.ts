import { describe, it, expect } from "vitest";
import { parseQuickAdd } from "../lib/quickparse";

describe("parseQuickAdd", () => {
  it("pulls priority, due, project and tags out of the title", () => {
    const r = parseQuickAdd("pay rent due:fri !high @home +life");
    expect(r.title).toBe("pay rent");
    expect(r.priority).toBe("A");
    expect(r.due).toBe("fri");
    expect(r.project).toBe("life");
    expect(r.tags).toEqual(["home"]);
  });

  it("maps the named priority markers and single letters", () => {
    expect(parseQuickAdd("x !high").priority).toBe("A");
    expect(parseQuickAdd("x !med").priority).toBe("B");
    expect(parseQuickAdd("x !low").priority).toBe("C");
    expect(parseQuickAdd("x !m").priority).toBe("B");
    expect(parseQuickAdd("x !d").priority).toBe("D");
  });

  it("honours a leading (A) todo.txt priority and ignores a later marker", () => {
    const r = parseQuickAdd("(B) ship it !high");
    expect(r.priority).toBe("B"); // first one wins, like the server
    expect(r.title).toBe("ship it !high"); // the marker stays since priority was set
  });

  it("does NOT treat a lowercase (a) as a priority (matches server isPriority)", () => {
    const r = parseQuickAdd("(a) ship it");
    expect(r.priority).toBeUndefined();
    expect(r.title).toBe("(a) ship it"); // left as text, like the server
  });

  it("ignores a key:value whose value starts with '/' (matches server splitKV)", () => {
    const r = parseQuickAdd("read due://notnow docs");
    expect(r.due).toBeUndefined();
    expect(r.title).toContain("due://notnow"); // kept as text, not a chip
  });

  it("captures start, recurrence and estimate keys", () => {
    const r = parseQuickAdd("standup t:tomorrow rec:weekly est:30m");
    expect(r.start).toBe("tomorrow");
    expect(r.recur).toBe("weekly");
    expect(r.est).toBe("30m");
    expect(r.title).toBe("standup");
  });

  it("collects wikilinks and de-dupes tags", () => {
    const r = parseQuickAdd("fix [[jwt-expiry]] @auth @auth @backend");
    expect(r.links).toEqual(["jwt-expiry"]);
    expect(r.tags).toEqual(["auth", "backend"]);
    expect(r.title).toBe("fix");
  });

  it("keeps the first project and leaves unknown key:values in the title", () => {
    const r = parseQuickAdd("note +api +infra src:claude do thing");
    expect(r.project).toBe("api");
    expect(r.title).toContain("src:claude");
    expect(r.title).toContain("do thing");
  });

  it("treats a bare title as just a title", () => {
    const r = parseQuickAdd("buy milk");
    expect(r.title).toBe("buy milk");
    expect(r.priority).toBeUndefined();
    expect(r.due).toBeUndefined();
    expect(r.tags).toEqual([]);
  });

  it("is empty for empty input", () => {
    const r = parseQuickAdd("   ");
    expect(r.title).toBe("");
    expect(r.tags).toEqual([]);
    expect(r.links).toEqual([]);
  });
});
