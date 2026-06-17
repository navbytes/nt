import type { CompletionContext, CompletionResult } from "@codemirror/autocomplete";
import type { NoteLink } from "./api";

// makeWikilinkSource returns a CodeMirror completion source that fires inside an
// open `[[` and suggests notes from the index. Selecting one inserts the note's
// title and closes the link — matching nt's [[Title]] / [[slug]] resolution.
export function makeWikilinkSource(getNotes: () => NoteLink[]) {
  return (ctx: CompletionContext): CompletionResult | null => {
    const before = ctx.matchBefore(/\[\[[^\]\n]*$/);
    if (!before) return null;
    const q = before.text.slice(2).toLowerCase();
    // If the user already typed a closer right after the cursor (`[[Title]` or
    // `[[Title]]`), don't append our own — that would produce `[[Title]]]`.
    const after = ctx.state.sliceDoc(ctx.pos, ctx.pos + 2);
    const closer = after.startsWith("]]") ? "" : after.startsWith("]") ? "]" : "]]";
    const matches = getNotes().filter(
      (n) => !q || n.title.toLowerCase().includes(q) || n.path.toLowerCase().includes(q),
    );
    if (matches.length === 0) {
      // A disabled hint reads better than the popup vanishing — the user knows
      // the source fired and simply has no match yet.
      return {
        from: before.from + 2,
        options: [{ label: "No matching notes", apply: () => {}, type: "text" }],
        validFor: /^[^\]\n]*$/,
      };
    }
    const options = matches
      .slice(0, 20)
      .map((n) => ({ label: n.title, detail: n.path, type: "text", apply: n.title + closer }));
    return { from: before.from + 2, options, validFor: /^[^\]\n]*$/ };
  };
}
