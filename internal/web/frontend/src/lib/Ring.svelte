<script lang="ts">
  // Ring — a hand-rolled SVG progress donut (no deps). A full track circle sits
  // under a foreground arc that sweeps from the top (12 o'clock) clockwise,
  // stroked with the signature spectral gradient. The arc length is driven by
  // `value` (0..1); `over` recolours it to the alert ramp when work exceeds the
  // budget. Purely decorative geometry — accessible text lives in the slot the
  // caller renders in the center, so the gradient never sits under reading text.
  //
  // Reusable: any 0..1 ratio (capacity, completion, progress) can drive it.
  // Reduced-motion safe: the only animation is the dash-offset sweep, guarded
  // below so it's instant (not removed) when the user prefers reduced motion.

  let {
    value = 0,
    size = 132,
    stroke = 12,
    over = false,
    /** A stable id is required so the <linearGradient> defs don't collide when
     *  several rings render on one page. */
    gradientId = "ring-grad",
    /** Decorative label for assistive tech describing the whole figure. */
    label = "",
  }: {
    value?: number;
    size?: number;
    stroke?: number;
    over?: boolean;
    gradientId?: string;
    label?: string;
  } = $props();

  const clamped = $derived(Math.max(0, Math.min(1, value)));
  const r = $derived((size - stroke) / 2);
  const cx = $derived(size / 2);
  const circ = $derived(2 * Math.PI * r);
  // Dash gap = the un-filled remainder, so the visible dash equals value·circ.
  const dashOffset = $derived(circ * (1 - clamped));
</script>

<svg
  class="ring"
  width={size}
  height={size}
  viewBox="0 0 {size} {size}"
  role={label ? "img" : "presentation"}
  aria-label={label || undefined}
  aria-hidden={label ? undefined : "true"}
>
  <defs>
    <linearGradient id={gradientId} x1="0%" y1="0%" x2="100%" y2="100%">
      {#if over}
        <stop offset="0%" stop-color="var(--red)" />
        <stop offset="100%" stop-color="color-mix(in srgb, var(--red) 60%, var(--orange, #a8620a))" />
      {:else}
        <stop offset="0%" stop-color="var(--spectral-1)" />
        <stop offset="52%" stop-color="var(--spectral-2)" />
        <stop offset="100%" stop-color="var(--spectral-3)" />
      {/if}
    </linearGradient>
  </defs>

  <!-- track -->
  <circle {cx} cy={cx} {r} fill="none" stroke="var(--viz-track)" stroke-width={stroke} />

  <!-- progress arc: rotate -90° so the dash starts at 12 o'clock and runs CW -->
  <circle
    class="ring__arc"
    {cx}
    cy={cx}
    {r}
    fill="none"
    stroke="url(#{gradientId})"
    stroke-width={stroke}
    stroke-linecap="round"
    stroke-dasharray={circ}
    stroke-dashoffset={dashOffset}
    transform="rotate(-90 {cx} {cx})"
  />
</svg>

<style>
  .ring {
    display: block;
    flex: none;
  }
  /* Animate the sweep as the value settles in. transform/opacity only would be
     ideal, but a stroke-dashoffset tween is GPU-cheap on a single path and the
     global reduced-motion rule already zeroes transition-duration; we add an
     explicit guard too so intent is local and obvious. */
  .ring__arc {
    transition: stroke-dashoffset var(--motion-slow) var(--ease-out);
  }
  @media (prefers-reduced-motion: reduce) {
    .ring__arc {
      transition: none;
    }
  }
</style>
