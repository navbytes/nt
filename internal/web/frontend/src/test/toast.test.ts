import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { toast, showToast, dismissToast } from "../lib/toast.svelte";

describe("toast store", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    dismissToast();
  });
  afterEach(() => vi.useRealTimers());

  it("shows a message, with an Undo handler when given", () => {
    const undo = vi.fn();
    showToast("Completed “pay rent”", undo);
    expect(toast.current?.message).toBe("Completed “pay rent”");
    expect(toast.current?.undo).toBe(undo);
  });

  it("latest toast wins and resets the clock", () => {
    showToast("first");
    vi.advanceTimersByTime(5000);
    showToast("second");
    vi.advanceTimersByTime(5000); // 10s after first, 5s after second
    expect(toast.current?.message).toBe("second"); // first's timer must not kill it
  });

  it("auto-dismisses after the ttl", () => {
    showToast("bye", undefined, 6000);
    vi.advanceTimersByTime(5999);
    expect(toast.current).not.toBeNull();
    vi.advanceTimersByTime(1);
    expect(toast.current).toBeNull();
  });

  it("dismisses on demand and cancels the timer", () => {
    showToast("x");
    dismissToast();
    expect(toast.current).toBeNull();
    vi.advanceTimersByTime(10000); // no late timer surprises
    expect(toast.current).toBeNull();
  });
});
