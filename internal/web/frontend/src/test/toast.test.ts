import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  toast,
  showToast,
  dismissToast,
  clearUndoToast,
  pauseToast,
  resumeToast,
  MAX_VISIBLE,
} from "../lib/toast.svelte";

describe("toast store", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    dismissToast(); // clear the whole stack
  });
  afterEach(() => vi.useRealTimers());

  it("shows a message, with an Undo handler when given", () => {
    const undo = vi.fn();
    showToast("Completed “pay rent”", undo);
    expect(toast.current?.message).toBe("Completed “pay rent”");
    expect(toast.current?.undo).toBe(undo);
  });

  it("stacks multiple toasts (newest is current), preserving earlier ones", () => {
    showToast("first");
    showToast("second");
    showToast("third");
    expect(toast.items.map((t) => t.message)).toEqual(["first", "second", "third"]);
    expect(toast.current?.message).toBe("third"); // newest is the accessor
  });

  it("each toast keeps its own ttl clock (no shared single-slot timer)", () => {
    showToast("first");
    vi.advanceTimersByTime(5000);
    showToast("second");
    vi.advanceTimersByTime(1000); // first hits 6s → drops; second at 1s → stays
    expect(toast.items.map((t) => t.message)).toEqual(["second"]);
    vi.advanceTimersByTime(5000); // second hits 6s → drops
    expect(toast.items).toHaveLength(0);
  });

  it("caps the visible stack and evicts the oldest", () => {
    for (let i = 0; i < MAX_VISIBLE + 2; i++) showToast(`t${i}`);
    expect(toast.items).toHaveLength(MAX_VISIBLE);
    // oldest two (t0, t1) evicted; newest MAX_VISIBLE remain in order
    expect(toast.items[0]?.message).toBe("t2");
    expect(toast.current?.message).toBe(`t${MAX_VISIBLE + 1}`);
  });

  it("auto-dismisses after the ttl", () => {
    showToast("bye", undefined, 6000);
    vi.advanceTimersByTime(5999);
    expect(toast.items).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(toast.items).toHaveLength(0);
    expect(toast.current).toBeNull();
  });

  it("dismisses one toast by id without touching the others", () => {
    const a = showToast("a");
    showToast("b");
    dismissToast(a);
    expect(toast.items.map((t) => t.message)).toEqual(["b"]);
  });

  it("clearUndoToast drops every toast that carries an Undo (a fresh write landed)", () => {
    showToast("Completed “x”", vi.fn());
    clearUndoToast();
    expect(toast.items).toHaveLength(0);
    expect(toast.current).toBeNull();
  });

  it("clearUndoToast leaves plain confirmation toasts alone but removes undo ones", () => {
    showToast("Saved"); // no undo handler — informational, stays
    showToast("Completed “y”", vi.fn()); // undo — must be dropped on a new write
    clearUndoToast();
    expect(toast.items.map((t) => t.message)).toEqual(["Saved"]);
  });

  it("a NEW write (clearUndoToast) keeps undo single-flight even as info toasts stack", () => {
    const firstUndo = vi.fn();
    showToast("Completed “a”", firstUndo);
    // a fresh write lands: caller clears the stale undo, then posts its own
    clearUndoToast();
    showToast("Completed “b”", vi.fn());
    const undos = toast.items.filter((t) => t.undo);
    expect(undos).toHaveLength(1);
    expect(undos[0]?.message).toBe("Completed “b”");
  });

  it("pauseToast freezes the dismiss clock; the toast survives past its ttl", () => {
    showToast("Completed “z”", vi.fn(), 6000);
    const id = toast.current!.id;
    vi.advanceTimersByTime(3000);
    pauseToast(id); // user reaches for Undo (hover/focus)
    vi.advanceTimersByTime(60000); // long pause — must NOT auto-dismiss
    expect(toast.items).toHaveLength(1);
  });

  it("resumeToast restarts the ttl from the top after a pause", () => {
    showToast("Completed “z”", vi.fn(), 6000);
    const id = toast.current!.id;
    pauseToast(id);
    vi.advanceTimersByTime(10000);
    resumeToast(id);
    vi.advanceTimersByTime(5999);
    expect(toast.items).toHaveLength(1); // fresh full window, not yet elapsed
    vi.advanceTimersByTime(1);
    expect(toast.items).toHaveLength(0);
  });

  it("resumeToast is a no-op for an already-dismissed toast", () => {
    const id = showToast("gone", vi.fn(), 6000);
    dismissToast(id);
    resumeToast(id); // must not resurrect or schedule a stray timer
    expect(toast.items).toHaveLength(0);
    vi.advanceTimersByTime(10000);
    expect(toast.items).toHaveLength(0);
  });

  it("dismiss with no id clears the whole stack and cancels timers", () => {
    showToast("x");
    showToast("y");
    dismissToast();
    expect(toast.items).toHaveLength(0);
    vi.advanceTimersByTime(10000); // no late timer surprises
    expect(toast.items).toHaveLength(0);
  });
});
