import { describe, it, expect } from "vitest";
import { makeWikilinkSource } from "../lib/cmWikilink";
import type { NoteLink } from "../lib/api-types";

const NOTES: NoteLink[] = [
  { url: "/n/a", title: "Architecture", path: "design/architecture.md" },
  { url: "/n/b", title: "Auth Design", path: "design/auth.md" },
  { url: "/n/c", title: "Meeting Notes", path: "meeting.md" },
];

// A minimal CompletionContext: only matchBefore is used by the source.
function ctx(textBeforeCursor: string) {
  return {
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
});
