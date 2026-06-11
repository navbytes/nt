import { describe, it, expect } from "vitest";
import { stepId } from "../lib/listnav";

describe("stepId", () => {
  const ids = ["a", "b", "c"];

  it("enters the list from nothing: j → first, k → last", () => {
    expect(stepId(ids, null, 1)).toBe("a");
    expect(stepId(ids, null, -1)).toBe("c");
  });

  it("moves down and up by one", () => {
    expect(stepId(ids, "a", 1)).toBe("b");
    expect(stepId(ids, "b", 1)).toBe("c");
    expect(stepId(ids, "c", -1)).toBe("b");
  });

  it("clamps at the ends without wrapping", () => {
    expect(stepId(ids, "c", 1)).toBe("c");
    expect(stepId(ids, "a", -1)).toBe("a");
  });

  it("re-enters from the top if the current id has vanished (after a mutation)", () => {
    expect(stepId(ids, "gone", 1)).toBe("a");
  });

  it("returns null for an empty list", () => {
    expect(stepId([], null, 1)).toBeNull();
    expect(stepId([], "a", -1)).toBeNull();
  });
});
