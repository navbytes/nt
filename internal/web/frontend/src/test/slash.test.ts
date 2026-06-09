import { describe, it, expect } from "vitest";
import { slashSource, slashLabels } from "../lib/cmSlash";

function ctx(textBeforeCursor: string) {
  return {
    pos: textBeforeCursor.length,
    matchBefore(re: RegExp) {
      const m = textBeforeCursor.match(re);
      if (!m) return null;
      const from = textBeforeCursor.length - m[0].length;
      return { from, to: textBeforeCursor.length, text: m[0] };
    },
  } as unknown as Parameters<typeof slashSource>[0];
}

describe("slashSource", () => {
  it("offers commands after a / at word start", () => {
    const res = slashSource(ctx("/"));
    expect(res).not.toBeNull();
    expect(res!.options.map((o) => o.label)).toEqual(expect.arrayContaining(["/todo", "/table", "/h1"]));
    // `from` anchors at the slash so the typed /cmd is replaced.
    expect(res!.from).toBe(0);
  });

  it("filters by the typed prefix and anchors after whitespace", () => {
    const res = slashSource(ctx("see /tab"));
    expect(res).not.toBeNull();
    expect(res!.from).toBe("see ".length); // the slash, not the space
  });

  it("only fires at word start (not mid-word / URL slashes)", () => {
    expect(slashSource(ctx("hello"))).toBeNull();
    expect(slashSource(ctx("a/b/c"))).toBeNull(); // a path-like slash must not trigger
    expect(slashSource(ctx("http://x"))).toBeNull();
  });

  it("exposes a stable command set", () => {
    expect(slashLabels).toContain("/date");
    expect(slashLabels.length).toBeGreaterThan(8);
  });
});
