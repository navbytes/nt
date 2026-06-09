import {
  snippetCompletion,
  type Completion,
  type CompletionContext,
  type CompletionResult,
} from "@codemirror/autocomplete";
import type { EditorView } from "@codemirror/view";

// Slash commands for the note editor (W6): type "/" at the start of a word and
// pick a markdown block to insert. Built on CodeMirror's snippet system, so
// templates with ${placeholders} drop the cursor in the right spot.
type Slash = { label: string; detail: string; template?: string; insert?: () => string };

const COMMANDS: Slash[] = [
  { label: "/h1", detail: "Heading 1", template: "# #{}" },
  { label: "/h2", detail: "Heading 2", template: "## #{}" },
  { label: "/h3", detail: "Heading 3", template: "### #{}" },
  { label: "/todo", detail: "Task item", template: "- [ ] #{}" },
  { label: "/bullet", detail: "Bullet list", template: "- #{}" },
  { label: "/number", detail: "Numbered list", template: "1. #{}" },
  { label: "/quote", detail: "Blockquote", template: "> #{}" },
  { label: "/code", detail: "Code block", template: "```#{lang}\n#{}\n```" },
  { label: "/table", detail: "Table", template: "| #{col1} | #{col2} |\n| --- | --- |\n| #{} |  |" },
  { label: "/hr", detail: "Divider", template: "---\n\n#{}" },
  { label: "/link", detail: "Link", template: "[#{text}](#{url})" },
  { label: "/date", detail: "Today's date", insert: () => new Date().toISOString().slice(0, 10) },
  { label: "/time", detail: "Current time", insert: () => new Date().toTimeString().slice(0, 5) },
];

// CodeMirror's snippet template uses ${...}; we author with #{...} above so the
// braces read cleanly, then convert here.
function tmpl(s: string): string {
  return s.replace(/#\{/g, "${");
}

export function slashSource(ctx: CompletionContext): CompletionResult | null {
  const before = ctx.matchBefore(/(?:^|\s)\/\w*$/);
  if (before == null) return null;
  const from = before.from + before.text.lastIndexOf("/"); // replace from the slash only
  const options: Completion[] = COMMANDS.map((c) =>
    c.template
      ? snippetCompletion(tmpl(c.template), { label: c.label, detail: c.detail, type: "keyword" })
      : {
          label: c.label,
          detail: c.detail,
          type: "keyword",
          apply: (view: EditorView, _c: Completion, f: number, t: number) =>
            view.dispatch({ changes: { from: f, to: t, insert: c.insert!() } }),
        },
  );
  return { from, options, validFor: /^\/\w*$/ };
}

// labels is exported for unit testing the command set.
export const slashLabels = COMMANDS.map((c) => c.label);
