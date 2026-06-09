import { describe, it, expect } from "vitest";
import { spriteKey, drawRadius, GLOW_SPREAD } from "../lib/graphSprites";
import { withAlpha } from "../lib/graphColors";

describe("spriteKey", () => {
  it("is stable for identical inputs", () => {
    expect(spriteKey("dark", "circle", "#7aa2f7", "bright")).toBe(
      spriteKey("dark", "circle", "#7aa2f7", "bright"),
    );
  });

  it("changes when any field changes (theme/shape/color/variant are all keyed)", () => {
    const base = spriteKey("dark", "circle", "#7aa2f7", "bright");
    expect(spriteKey("light", "circle", "#7aa2f7", "bright")).not.toBe(base);
    expect(spriteKey("dark", "diamond", "#7aa2f7", "bright")).not.toBe(base);
    expect(spriteKey("dark", "circle", "#ff9e64", "bright")).not.toBe(base);
    expect(spriteKey("dark", "circle", "#7aa2f7", "focus")).not.toBe(base);
  });
});

describe("drawRadius", () => {
  it("expands a node radius by the glow padding and is monotonic", () => {
    expect(drawRadius(0)).toBe(0);
    expect(drawRadius(10)).toBeCloseTo(10 * (1 + GLOW_SPREAD));
    expect(drawRadius(12)).toBeGreaterThan(drawRadius(6));
  });
});

describe("withAlpha", () => {
  it("converts #rrggbb to rgba()", () => {
    expect(withAlpha("#7aa2f7", 0.5)).toBe("rgba(122,162,247,0.5)");
  });

  it("expands #rgb shorthand", () => {
    expect(withAlpha("#abc", 1)).toBe("rgba(170,187,204,1)");
  });

  it("returns non-hex input unchanged", () => {
    expect(withAlpha("rgba(1,2,3,0.4)", 0.2)).toBe("rgba(1,2,3,0.4)");
  });
});
