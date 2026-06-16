<script lang="ts">
  // Sparkline — a hand-rolled inline-SVG trend (no deps): a soft spectral area
  // under a spectral stroke, with a dot on the most recent point. Given a series
  // of numbers it maps them across a fixed viewBox; the caller supplies real
  // values only (this component invents nothing). A flat all-zero series renders
  // a calm baseline rather than a spike, so an empty week reads as "quiet", not
  // broken.
  //
  // Reusable + theme-driven: any numeric series (completions/day, activity/day)
  // can drive it. No animation, so nothing to guard for reduced-motion.

  let {
    data = [],
    width = 240,
    height = 56,
    /** Stable id so the gradient/clip defs don't collide across instances. */
    gradientId = "spark-grad",
    label = "",
  }: {
    data?: number[];
    width?: number;
    height?: number;
    gradientId?: string;
    label?: string;
  } = $props();

  const pad = 3; // keep the round stroke + end dot inside the viewBox
  const max = $derived(Math.max(1, ...data)); // ≥1 so a zero series sits on the floor
  const n = $derived(data.length);

  // X across the full width; Y inverted (SVG y grows downward). With one point
  // we still place it (mid-width) so a single day of data isn't blank.
  const xAt = (i: number): number => (n <= 1 ? width / 2 : pad + (i * (width - pad * 2)) / (n - 1));
  const yAt = (v: number): number => height - pad - (v / max) * (height - pad * 2);

  const pts = $derived(data.map((v, i) => [xAt(i), yAt(v)] as const));
  const linePath = $derived(
    pts.length ? pts.map(([x, y], i) => `${i ? "L" : "M"}${x.toFixed(1)} ${y.toFixed(1)}`).join(" ") : "",
  );
  // Close the area down to the baseline and back to the start for the fill.
  const areaPath = $derived(
    pts.length
      ? `${linePath} L${pts[pts.length - 1]![0].toFixed(1)} ${height - pad} L${pts[0]![0].toFixed(1)} ${height - pad} Z`
      : "",
  );
  const last = $derived(pts.length ? pts[pts.length - 1]! : null);
</script>

<svg
  class="spark"
  {width}
  {height}
  viewBox="0 0 {width} {height}"
  preserveAspectRatio="none"
  role={label ? "img" : "presentation"}
  aria-label={label || undefined}
  aria-hidden={label ? undefined : "true"}
>
  <defs>
    <linearGradient id={gradientId} x1="0%" y1="0%" x2="0%" y2="100%">
      <stop offset="0%" stop-color="var(--spectral-2)" stop-opacity="0.28" />
      <stop offset="100%" stop-color="var(--spectral-2)" stop-opacity="0" />
    </linearGradient>
  </defs>

  {#if areaPath}
    <path d={areaPath} fill="url(#{gradientId})" stroke="none" />
  {/if}
  {#if linePath}
    <path
      d={linePath}
      fill="none"
      stroke="var(--spectral-2)"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
      vector-effect="non-scaling-stroke"
    />
  {/if}
  {#if last}
    <circle cx={last[0]} cy={last[1]} r="2.6" fill="var(--spectral-3)" />
  {/if}
</svg>

<style>
  .spark {
    display: block;
    width: 100%;
    height: auto;
    overflow: visible;
  }
</style>
