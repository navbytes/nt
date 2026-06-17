// parseQuickAdd mirrors the server's quick-add grammar (internal/quickadd +
// task.ParseLine) closely enough to show a live "here's what I understood"
// preview under the add box — the capture affordance best-in-class task apps lean
// on. It deliberately does NOT resolve natural-language dates to ISO (that lives
// in Go's dateparse); it surfaces the token you typed so the preview can never
// drift from, or lie about, what the server will actually do.

export interface QuickAdd {
  /** The task title with all recognised tokens stripped out. */
  title: string;
  /** "A".."Z" from a leading (A) or a !high/!med/!low/!x marker. */
  priority?: string;
  /** Raw value after due: (e.g. "fri", "tomorrow", "2026-06-20T17:00"). */
  due?: string;
  /** Raw value after t: (start/defer date). */
  start?: string;
  /** Raw value after rec: (recurrence). */
  recur?: string;
  /** Raw value after est: (time estimate). */
  est?: string;
  /** First +project. */
  project?: string;
  /** @tags, in order, de-duplicated. */
  tags: string[];
  /** [[wikilink]] targets. */
  links: string[];
  /** Recognised keys typed with no value yet (e.g. "due:" while still typing) —
   *  surfaced so the preview can hint "due needs a value" instead of silently
   *  dropping the token into the title. */
  emptyKeys: string[];
}

// The recognised value-keys (and their aliases) the quick-add grammar resolves.
const VALUE_KEYS = new Set(["due", "t", "rec", "est"]);

const PRI_WORDS: Record<string, string> = {
  high: "A",
  h: "A",
  med: "B",
  medium: "B",
  m: "B",
  low: "C",
  l: "C",
};

// priFromWord maps a !marker body to a priority letter, matching dateparse.Priority:
// the named levels, or any single A–Z letter as a literal todo.txt priority.
function priFromWord(w: string): string | null {
  const lw = w.toLowerCase();
  if (lw in PRI_WORDS) return PRI_WORDS[lw]!;
  if (/^[a-z]$/i.test(w)) return w.toUpperCase();
  return null;
}

export function parseQuickAdd(text: string): QuickAdd {
  const out: QuickAdd = { title: "", tags: [], links: [], emptyKeys: [] };
  let s = text.trim();

  // A leading (A)–(Z) is a literal todo.txt priority. Only UPPERCASE counts —
  // task.go's isPriority rejects "(a)", leaving it as text — so the preview must
  // too, or it would show a priority the server won't apply.
  const lead = /^\(([A-Z])\)\s+/.exec(s);
  if (lead) {
    out.priority = lead[1]!;
    s = s.slice(lead[0].length);
  }

  const titleWords: string[] = [];
  let priFound = out.priority !== undefined;
  for (const w of s.split(/\s+/).filter(Boolean)) {
    if (/^\[\[.+\]\]$/.test(w)) {
      out.links.push(w.slice(2, -2));
      continue;
    }
    // The !marker only lifts the first time and only when no priority is set yet.
    if (!priFound && w.length > 1 && w[0] === "!") {
      const p = priFromWord(w.slice(1));
      if (p) {
        out.priority = p;
        priFound = true;
        continue;
      }
    }
    if (w.length > 1 && w[0] === "+") {
      out.project ??= w.slice(1); // first +project wins (the DTO carries one)
      continue;
    }
    if (w.length > 1 && w[0] === "@") {
      const tag = w.slice(1);
      if (!out.tags.includes(tag)) out.tags.push(tag);
      continue;
    }
    // A recognised value-key with nothing after the colon ("due:") is mid-type —
    // surface it as a hint rather than dropping the token into the title.
    const bare = /^([a-zA-Z][\w-]*):$/.exec(w);
    if (bare && VALUE_KEYS.has(bare[1]!.toLowerCase())) {
      const k = bare[1]!.toLowerCase();
      if (!out.emptyKeys.includes(k)) out.emptyKeys.push(k);
      continue;
    }
    // Mirror task.go's splitKV: a value starting with "/" (URL-ish) is NOT a
    // key:value — the server keeps the whole token as text, so we do too.
    const kv = /^([a-zA-Z][\w-]*):(.+)$/.exec(w);
    if (kv && !kv[2]!.startsWith("/")) {
      const k = kv[1]!.toLowerCase();
      const v = kv[2]!;
      if (k === "due") {
        out.due = v;
        continue;
      }
      if (k === "t") {
        out.start = v;
        continue;
      }
      if (k === "rec") {
        out.recur = v;
        continue;
      }
      if (k === "est") {
        out.est = v;
        continue;
      }
      // Unknown key:value (id:, src:, …) is metadata we don't preview — keep it in
      // the title so nothing the user typed silently vanishes from the preview.
    }
    titleWords.push(w);
  }

  out.title = titleWords.join(" ");
  return out;
}
