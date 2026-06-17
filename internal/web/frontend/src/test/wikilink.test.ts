import { describe, it, expect } from "vitest";
import { makeWikilinkSource } from "../lib/cmWikilink";
import type { NoteLink } from "../lib/api-types";

const NOTES: NoteLink[] = [
  { url: "/n/a", title: "Architecture", path: "design/architecture.md" },
  { url: "/n/b", title: "Auth Design", path: "design/auth.md" },
  { url: "/n/c", title: "Meeting Notes", path: "meeting.md" },
];

// A minimal CompletionContext: matchBefore + pos + state.sliceDoc are used by the
// source. `after` is the text immediately following the cursor (for the existing
// `]`/`]]` closer detection).
function ctx(textBeforeCursor: string, after = "") {
  const full = textBeforeCursor + after;
  return {
    pos: textBeforeCursor.length,
    state: {
      sliceDoc(from: number, to: number) {
        return full.slice(from, to);
      },
    },
    matchBefore(re: RegExp) {
      const m = textBeforeCursor.match(re);
      if (!m) return null;
      const from = textBeforeCursor.length - m[0].length;
      return { from, to: textBeforeCursor.length, text: m[0] };
    },
  } as unknown as Parameters<ReturnType<typeof makeWikilinkSource>>[0];
}

describe("makeWikilinkSource", () => {
  const source = makeWikilinkSource(() => NOTES);

  it("returns null when not inside an open [[", () => {
    expect(source(ctx("just some text"))).toBeNull();
    expect(source(ctx("a closed [[link]] here"))).toBeNull();
  });

  it("suggests matching notes inside [[ and inserts Title]]", () => {
    const res = source(ctx("see [[Arch"));
    expect(res).not.toBeNull();
    const opts = res!.options;
    expect(opts.map((o) => o.label)).toContain("Architecture");
    expect(opts.find((o) => o.label === "Architecture")!.apply).toBe("Architecture]]");
    // `from` points just after the `[[` so the typed query is replaced.
    expect(res!.from).toBe("see ".length + 2);
  });

  it("matches on title or path, case-insensitively", () => {
    expect(source(ctx("[[design"))!.options.map((o) => o.label)).toEqual(
      expect.arrayContaining(["Architecture", "Auth Design"]),
    );
  });

  it("lists all notes for an empty query right after [[", () => {
    expect(source(ctx("[["))!.options).toHaveLength(NOTES.length);
  });

  it("does not double the closer when ]] already follows the cursor", () => {
    const res = source(ctx("see [[Arch", "]]"));
    expect(res!.options.find((o) => o.label === "Architecture")!.apply).toBe("Architecture");
  });

  it("appends only one ] when a single ] already follows", () => {
    const res = source(ctx("see [[Arch", "]"));
    expect(res!.options.find((o) => o.label === "Architecture")!.apply).toBe("Architecture]");
  });

  it("returns a disabled hint instead of null when nothing matches", () => {
    const res = source(ctx("[[zzzznope"));
    expect(res).not.toBeNull();
    expect(res!.options).toHaveLength(1);
    expect(res!.options[0]!.label).toBe("No matching notes");
  });
});
