import { describe, it, expect } from "vitest";
import { displayTitle } from "../lib/text";

describe("displayTitle", () => {
  it("returns short text unchanged", () => {
    expect(displayTitle("pay rent", 60)).toBe("pay rent");
  });

  it("cuts at the first sentence/clause boundary", () => {
    const t = "Investigate the flaky auth test — it's probably a token race, then fix it";
    expect(displayTitle(t, 40)).toBe("Investigate the flaky auth test —");
  });

  it("falls back to a word boundary with an ellipsis", () => {
    const t = "one two three four five six seven eight nine ten eleven twelve";
    const out = displayTitle(t, 20);
    expect(out.endsWith("…")).toBe(true);
    expect(out.length).toBeLessThanOrEqual(21);
    expect(out).not.toMatch(/\s…$/); // trimmed before the ellipsis
    expect(t.startsWith(out.replace("…", ""))).toBe(true); // never invents text
  });

  it("collapses whitespace", () => {
    expect(displayTitle("a   b\n c", 60)).toBe("a b c");
  });
});
