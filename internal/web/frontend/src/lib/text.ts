// displayTitle derives a short, scannable title from a task/note's full text —
// for places that can't wrap (graph node labels) or where a long line looks
// unpolished. Agents often capture a whole sentence as a task's only text
// (todo.txt has no title/body split), so we show the first clause and keep the
// full text one hover/expand away (never discarded).
//
// It prefers the first sentence/clause boundary (. ? ! — : ;), falling back to a
// hard cut at the last word boundary before max. The result never exceeds max+1
// (the ellipsis).
export function displayTitle(text: string, max = 60): string {
  const t = text.trim().replace(/\s+/g, " ");
  if (t.length <= max) return t;

  // First sentence/clause boundary within a sensible window.
  const window = t.slice(0, max + 1);
  const clause = window.search(/[.?!—:;](\s|$)/);
  if (clause >= 12) return t.slice(0, clause + 1).trim();

  // Otherwise cut at the last word boundary before max.
  const cut = window.lastIndexOf(" ");
  return (cut >= 12 ? t.slice(0, cut) : t.slice(0, max)).trimEnd() + "…";
}
