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
    const options = getNotes()
      .filter((n) => !q || n.title.toLowerCase().includes(q) || n.path.toLowerCase().includes(q))
      .slice(0, 20)
      .map((n) => ({ label: n.title, detail: n.path, type: "text", apply: n.title + "]]" }));
    if (options.length === 0) return null;
    return { from: before.from + 2, options, validFor: /^[^\]\n]*$/ };
  };
}
