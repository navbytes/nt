import { describe, it, expect } from "vitest";
import {
  displayTitle,
  priorityClass,
  priorityRank,
  fmtTime,
  relativeDue,
  meaningfulSource,
  fmtDuration,
  highlightParts,
} from "../lib/text";

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

describe("priorityClass / priorityRank", () => {
  it("maps the first three letters to distinct classes", () => {
    expect(priorityClass("A")).toBe("a");
    expect(priorityClass("b")).toBe("b"); // case-insensitive
    expect(priorityClass("C")).toBe("c");
  });
  it("folds D–Z into one muted class and ignores junk", () => {
    expect(priorityClass("D")).toBe("rest");
    expect(priorityClass("Z")).toBe("rest");
    expect(priorityClass("")).toBe("");
    expect(priorityClass(undefined)).toBe("");
    expect(priorityClass("1")).toBe("");
  });
  it("ranks A highest and no-priority last", () => {
    expect(priorityRank("A")).toBe(0);
    expect(priorityRank("C")).toBe(2);
    expect(priorityRank(undefined)).toBe(99);
    expect(priorityRank("A")).toBeLessThan(priorityRank("B"));
    expect(priorityRank("Z")).toBeLessThan(priorityRank(undefined));
  });
});

describe("fmtTime", () => {
  it("renders terse 12h times", () => {
    expect(fmtTime("17:00")).toBe("5pm");
    expect(fmtTime("17:30")).toBe("5:30pm");
    expect(fmtTime("09:05")).toBe("9:05am");
    expect(fmtTime("00:00")).toBe("12am");
    expect(fmtTime("12:00")).toBe("12pm");
  });
  it("rejects nonsense", () => {
    expect(fmtTime("")).toBe("");
    expect(fmtTime("25:00")).toBe("");
    expect(fmtTime("nope")).toBe("");
  });
});

describe("relativeDue", () => {
  const now = new Date(2026, 5, 11); // Thu Jun 11 2026 (month is 0-based)

  it("names today, tomorrow, and yesterday", () => {
    expect(relativeDue("2026-06-11", now).label).toBe("Today");
    expect(relativeDue("2026-06-12", now).label).toBe("Tomorrow");
    expect(relativeDue("2026-06-10", now).label).toBe("Yesterday");
  });
  it("appends a time only for today/tomorrow", () => {
    expect(relativeDue("2026-06-11T17:00", now).label).toBe("Today 5pm");
    expect(relativeDue("2026-06-12T09:30", now).label).toBe("Tomorrow 9:30am");
    expect(relativeDue("2026-06-13T09:30", now).label).toBe("Sat"); // time dropped further out
  });
  it("uses the weekday for the coming week and Nd ago for the recent past", () => {
    expect(relativeDue("2026-06-13", now).label).toBe("Sat");
    expect(relativeDue("2026-06-08", now).label).toBe("3d ago");
  });
  it("falls back to an absolute date further out, with year only when it differs", () => {
    expect(relativeDue("2026-06-20", now).label).toBe("Jun 20");
    expect(relativeDue("2027-01-02", now).label).toBe("Jan 2 2027");
  });
  it("flags overdue (date-only) and soon", () => {
    expect(relativeDue("2026-06-10", now).overdue).toBe(true);
    expect(relativeDue("2026-06-11", now).overdue).toBe(false); // due today isn't overdue yet
    expect(relativeDue("2026-06-11", now).soon).toBe(true);
    expect(relativeDue("2026-06-12", now).soon).toBe(true);
    expect(relativeDue("2026-06-15", now).soon).toBe(false);
  });
});

describe("meaningfulSource", () => {
  it("hides default human/CLI origins", () => {
    for (const s of ["", "cli", "web", "tui", "user", "CLI"]) {
      expect(meaningfulSource(s)).toBe("");
    }
  });
  it("surfaces agent and other origins", () => {
    expect(meaningfulSource("claude")).toBe("claude");
    expect(meaningfulSource("cursor")).toBe("cursor");
  });
});

describe("fmtDuration", () => {
  it("renders compact h/m", () => {
    expect(fmtDuration(45)).toBe("45m");
    expect(fmtDuration(60)).toBe("1h");
    expect(fmtDuration(90)).toBe("1h 30m");
    expect(fmtDuration(120)).toBe("2h");
    expect(fmtDuration(0)).toBe("0m");
    expect(fmtDuration(-5)).toBe("0m");
  });
});

describe("highlightParts", () => {
  it("splits around case-insensitive matches", () => {
    const p = highlightParts("Fix the Token race", "token");
    expect(p).toEqual([
      { text: "Fix the ", hit: false },
      { text: "Token", hit: true },
      { text: " race", hit: false },
    ]);
  });
  it("handles multiple hits and a trailing match", () => {
    const p = highlightParts("aXaXa", "x");
    expect(p.filter((x) => x.hit).length).toBe(2);
    expect(p.map((x) => x.text).join("")).toBe("aXaXa");
  });
  it("empty query → one plain part", () => {
    expect(highlightParts("hello", "  ")).toEqual([{ text: "hello", hit: false }]);
  });
  it("no match → one plain part", () => {
    expect(highlightParts("hello", "zzz")).toEqual([{ text: "hello", hit: false }]);
  });
});
