import { describe, it, expect } from "vitest";
import { goTarget, isTextEntry, GO_CHORDS } from "../lib/keys.svelte";

describe("goTarget", () => {
  it("resolves every advertised go-to chord", () => {
    for (const g of GO_CHORDS) {
      expect(goTarget(g.key)).toBe(g.path);
    }
  });
  it("maps the documented mnemonics", () => {
    expect(goTarget("t")).toBe("/");
    expect(goTarget("a")).toBe("/tasks");
    expect(goTarget("r")).toBe("/review");
    expect(goTarget("g")).toBe("/graph");
  });
  it("is case-insensitive", () => {
    expect(goTarget("T")).toBe("/");
  });
  it("returns null for a non-target key so the chord is abandoned", () => {
    expect(goTarget("z")).toBeNull();
    expect(goTarget("1")).toBeNull();
    expect(goTarget("")).toBeNull();
  });
});

describe("isTextEntry", () => {
  it("is true for inputs, textareas, selects and contenteditable", () => {
    expect(isTextEntry(document.createElement("input"))).toBe(true);
    expect(isTextEntry(document.createElement("textarea"))).toBe(true);
    expect(isTextEntry(document.createElement("select"))).toBe(true);
    // jsdom doesn't derive isContentEditable from the attribute, so set the
    // property on a real element to exercise the contenteditable branch.
    const ce = document.createElement("div");
    Object.defineProperty(ce, "isContentEditable", { value: true });
    expect(isTextEntry(ce)).toBe(true);
  });
  it("is false for non-editable elements and non-elements", () => {
    expect(isTextEntry(document.createElement("button"))).toBe(false);
    expect(isTextEntry(document.createElement("div"))).toBe(false);
    expect(isTextEntry(null)).toBe(false);
  });
});
