// Pre-rendered "glowing orb" node sprites.
//
// Drawing a soft glow per node per frame (ctx.shadowBlur on the main canvas) is a
// Canvas2D perf cliff. Instead we bake each node's glow into an offscreen bitmap
// ONCE per (theme, shape, color, variant) and blit it with a plain drawImage in
// the hot loop — an O(1) Map lookup, honoring the graph's O(1)-accessor rule.
//
// The cache is naturally tiny: shapes(6) × palette(~13) × variants(2) × theme(1)
// ≈ a couple hundred bitmaps max, built lazily on first use and dropped on theme
// change (when the baked colors/glow no longer match).

import { tracePath, type ShapeKind } from "./graphShapes";
import { withAlpha } from "./graphColors";

export type SpriteVariant = "bright" | "focus";
export type SpriteTheme = "light" | "dark";

// ---- geometry (all in sprite-bitmap pixels) ----
// The shape is rendered at a fixed high resolution and scaled DOWN per node, so
// it stays crisp under zoom×devicePixelRatio. nodeRadius() in Graph.svelte caps
// at 12 world units; we render at that max and let drawImage shrink it.
const MAX_R_WORLD = 12;
const PX_PER_WORLD = 8; // crisp to ~zoom 4 on retina
const R_PX = MAX_R_WORLD * PX_PER_WORLD; // 96 — shape radius inside the sprite
export const GLOW_SPREAD = 0.8; // halo padding as a fraction of the shape radius
const S_PX = Math.round(R_PX * (1 + GLOW_SPREAD)); // 173 — sprite half-size
const SIZE = Math.min(512, S_PX * 2); // 346 — sprite is SIZE×SIZE px
const PAD_PX = S_PX - R_PX; // glow headroom before the bitmap edge

export interface SpriteStyle {
  theme: SpriteTheme;
  glowAlpha: number; // 0..1 base halo strength (from --graph-glow-alpha)
  glowBlur: number; // softness knob (from --graph-glow-blur)
}

// spriteKey is the cache key. Pure + deterministic so it's unit-testable without
// a canvas. Theme is part of the key because the same color glows differently on
// light vs dark.
export function spriteKey(
  theme: SpriteTheme,
  shape: ShapeKind,
  color: string,
  variant: SpriteVariant,
): string {
  return `${theme}:${shape}:${color}:${variant}`;
}

// drawRadius is the world-space half-size to blit a node of radius r: the shape
// fills `r`, the surrounding glow occupies the GLOW_SPREAD padding. Drawing the
// SIZE×SIZE bitmap into a 2*drawRadius box maps the baked shape back to ~r world
// units, so toggling effects on/off never changes a node's apparent size.
export function drawRadius(r: number): number {
  return r * (1 + GLOW_SPREAD);
}

type SpriteCanvas = HTMLCanvasElement;

export class SpriteCache {
  private map = new Map<string, SpriteCanvas>();
  private style: SpriteStyle;

  constructor(style: SpriteStyle) {
    this.style = style;
  }

  // setStyle swaps theme/glow params and drops every baked bitmap. Call it on
  // theme change (the cached glow colors no longer match).
  setStyle(style: SpriteStyle): void {
    this.style = style;
    this.map.clear();
  }

  clear(): void {
    this.map.clear();
  }

  // get returns the baked orb for (shape, color, variant), building it lazily.
  get(shape: ShapeKind, color: string, variant: SpriteVariant): SpriteCanvas {
    const key = spriteKey(this.style.theme, shape, color, variant);
    let c = this.map.get(key);
    if (!c) {
      c = buildSprite(shape, color, variant, this.style);
      this.map.set(key, c);
    }
    return c;
  }
}

// buildSprite bakes one glowing orb: a soft halo (shape-following, via shadowBlur)
// under a filled shape, finished with a faint upper-left sheen for an "orb" read.
function buildSprite(
  shape: ShapeKind,
  color: string,
  variant: SpriteVariant,
  style: SpriteStyle,
): HTMLCanvasElement {
  const cv = document.createElement("canvas");
  cv.width = SIZE;
  cv.height = SIZE;
  const ctx = cv.getContext("2d")!;
  const c = SIZE / 2;
  const dark = style.theme === "dark";
  const focus = variant === "focus";

  // --- glow halo (baked once; cheap to blit) ---
  if (style.glowAlpha > 0) {
    const alpha = Math.min(1, style.glowAlpha * (focus ? 1.45 : 1));
    const blur = Math.min(PAD_PX * 0.45, style.glowBlur * 2.4 * (focus ? 1.3 : 1));
    ctx.save();
    ctx.shadowColor = withAlpha(color, alpha);
    ctx.shadowBlur = blur;
    // The caster is the shape itself; its own fill is hidden by the body below,
    // leaving just the glow hugging the silhouette. A second, tighter pass
    // deepens the bloom (especially on dark / focus).
    tracePath(ctx, shape, c, c, R_PX);
    ctx.fillStyle = color;
    ctx.fill();
    if (dark || focus) {
      ctx.shadowBlur = blur * 0.55;
      ctx.fill();
    }
    ctx.restore();
  }

  // --- node body ---
  ctx.save();
  tracePath(ctx, shape, c, c, R_PX);
  ctx.fillStyle = color;
  ctx.fill();
  ctx.restore();

  // --- orb sheen: a soft highlight offset up-left, clipped to the shape ---
  const sheen = dark ? (focus ? 0.32 : 0.2) : focus ? 0.18 : 0.1;
  if (sheen > 0) {
    ctx.save();
    tracePath(ctx, shape, c, c, R_PX);
    ctx.clip();
    const g = ctx.createRadialGradient(
      c - R_PX * 0.35,
      c - R_PX * 0.4,
      R_PX * 0.1,
      c,
      c,
      R_PX * 1.15,
    );
    g.addColorStop(0, `rgba(255,255,255,${sheen})`);
    g.addColorStop(0.55, "rgba(255,255,255,0)");
    ctx.fillStyle = g;
    ctx.fillRect(c - R_PX, c - R_PX, R_PX * 2, R_PX * 2);
    ctx.restore();
  }

  return cv;
}
