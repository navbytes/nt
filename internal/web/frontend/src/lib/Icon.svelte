<script lang="ts">
  // SF-Symbols-style inline icon set. All icons are drawn on a 24×24 grid with
  // `stroke: currentColor` and round joins, so they inherit the text color of
  // whatever control they sit in (and theme for free) and read like Apple's
  // "small/regular" symbol weight. Use a `label` for standalone meaningful
  // icons; omit it (the default) when the parent control already has an
  // aria-label, so the glyph stays decorative for screen readers.
  let {
    name,
    size = 16,
    label = "",
    strokeWidth = 1.6,
    filled = false,
  }: {
    name: string;
    size?: number;
    label?: string;
    strokeWidth?: number;
    filled?: boolean;
  } = $props();
</script>

<svg
  class="icon"
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width={strokeWidth}
  stroke-linecap="round"
  stroke-linejoin="round"
  role={label ? "img" : "presentation"}
  aria-label={label || undefined}
  aria-hidden={label ? undefined : "true"}
>
  {#if name === "sidebar"}
    <rect x="3" y="4.5" width="18" height="15" rx="2.5" />
    <path d="M9 4.5v15" />
  {:else if name === "close"}
    <path d="M6 6l12 12M18 6L6 18" />
  {:else if name === "plus"}
    <path d="M12 5v14M5 12h14" />
  {:else if name === "search"}
    <circle cx="11" cy="11" r="7" />
    <path d="m20 20-3.6-3.6" />
  {:else if name === "help"}
    <circle cx="12" cy="12" r="9" />
    <path d="M9.6 9.3a2.4 2.4 0 1 1 3.3 2.2c-.7.3-1 .8-1 1.5v.3" />
    <circle cx="12" cy="16.4" r="0.65" fill="currentColor" stroke="none" />
  {:else if name === "sun"}
    <circle cx="12" cy="12" r="4" />
    <path
      d="M12 2.5v2M12 19.5v2M4.2 4.2l1.4 1.4M18.4 18.4l1.4 1.4M2.5 12h2M19.5 12h2M4.2 19.8l1.4-1.4M18.4 5.6l1.4-1.4"
    />
  {:else if name === "moon"}
    <path d="M20.5 13.5A8.5 8.5 0 1 1 10.5 3.5a6.6 6.6 0 0 0 10 10Z" />
  {:else if name === "warning"}
    <path d="M10.3 4 2.4 17.5A1.9 1.9 0 0 0 4 20.4h16a1.9 1.9 0 0 0 1.6-2.9L13.7 4a1.9 1.9 0 0 0-3.4 0Z" />
    <path d="M12 9.5v4" />
    <circle cx="12" cy="16.9" r="0.65" fill="currentColor" stroke="none" />
  {:else if name === "arrow-right"}
    <path d="M5 12h14M13 6l6 6-6 6" />
  {:else if name === "arrow-up"}
    <path d="M12 19V5M6 11l6-6 6 6" />
  {:else if name === "arrow-down"}
    <path d="M12 5v14M6 13l6 6 6-6" />
  {:else if name === "chevron-right"}
    <path d="m9 6 6 6-6 6" />
  {:else if name === "chevron-down"}
    <path d="m6 9 6 6 6-6" />
  {:else if name === "chevron-up"}
    <path d="m6 15 6-6 6 6" />
  {:else if name === "chevron-left"}
    <path d="m15 6-6 6 6 6" />
  {:else if name === "focus"}
    <circle cx="12" cy="12" r="3" />
    <path d="M12 3v3.2M12 17.8V21M3 12h3.2M17.8 12H21" />
  {:else if name === "check"}
    <path d="M5 12.5 10 17.5 19 6.5" />
  {:else if name === "star"}
    <path
      d="m12 3.5 2.6 5.3 5.8.9-4.2 4.1 1 5.8L12 16.9 6.8 19.6l1-5.8L3.6 9.7l5.8-.9Z"
      fill={filled ? "currentColor" : "none"}
    />
  {:else if name === "archive"}
    <rect x="3.5" y="4.5" width="17" height="4.5" rx="1.3" />
    <path d="M5 9V19a1.5 1.5 0 0 0 1.5 1.5h11A1.5 1.5 0 0 0 19 19V9M9.5 12.5h5" />
  {:else if name === "ellipsis"}
    <circle cx="5.5" cy="12" r="1.4" fill="currentColor" stroke="none" />
    <circle cx="12" cy="12" r="1.4" fill="currentColor" stroke="none" />
    <circle cx="18.5" cy="12" r="1.4" fill="currentColor" stroke="none" />
  {:else if name === "calendar"}
    <rect x="3.5" y="5" width="17" height="15.5" rx="2.5" />
    <path d="M3.5 9.5h17M8 3v4M16 3v4" />
  {:else if name === "tag"}
    <path
      d="M3.5 11.3V4.8a1.3 1.3 0 0 1 1.3-1.3h6.5a2 2 0 0 1 1.4.6l7 7a2 2 0 0 1 0 2.8l-5.8 5.8a2 2 0 0 1-2.8 0l-7-7a2 2 0 0 1-.6-1.4Z"
    />
    <circle cx="8" cy="8" r="1.4" fill="currentColor" stroke="none" />
  {:else if name === "return"}
    <path d="M20 6v3.5a3 3 0 0 1-3 3H4M9 7.5 4 12.5l5 5" />
  {:else if name === "doing"}
    <circle cx="12" cy="12" r="8.5" />
    <path d="M12 3.5a8.5 8.5 0 0 1 0 17Z" fill="currentColor" stroke="none" />
  {:else if name === "trash"}
    <path d="M4 7h16M9 7V5.2A1.6 1.6 0 0 1 10.6 3.6h2.8A1.6 1.6 0 0 1 15 5.2V7" />
    <path d="M6.5 7l.9 12.6a1.6 1.6 0 0 0 1.6 1.4h6a1.6 1.6 0 0 0 1.6-1.4L17.5 7" />
  {:else if name === "edit"}
    <path d="M4 20h4L18.5 9.5a2 2 0 0 0 0-2.8l-1.2-1.2a2 2 0 0 0-2.8 0L4 16v4Z" />
    <path d="M13.5 6.5 17.5 10.5" />
  {:else if name === "command"}
    <rect x="8.5" y="8.5" width="7" height="7" rx="0.5" />
    <path
      d="M8.5 8.5A2.5 2.5 0 1 0 6 11h2.5M15.5 8.5A2.5 2.5 0 1 1 18 11h-2.5M8.5 15.5A2.5 2.5 0 1 1 6 13h2.5M15.5 15.5A2.5 2.5 0 1 0 18 13h-2.5"
    />
  {:else if name === "square"}
    <rect x="4.5" y="4.5" width="15" height="15" rx="4" />
  {:else if name === "square-check"}
    <rect
      x="4.5"
      y="4.5"
      width="15"
      height="15"
      rx="4"
      fill={filled ? "currentColor" : "none"}
    />
    <path d="M8.5 12.2 11 14.7 15.7 9.4" stroke={filled ? "var(--on-accent, #fff)" : "currentColor"} />
  {:else if name === "repeat"}
    <path d="M17 4l3 3-3 3" />
    <path d="M20 7H9a5 5 0 0 0-5 5" />
    <path d="M7 20l-3-3 3-3" />
    <path d="M4 17h11a5 5 0 0 0 5-5" />
  {:else if name === "blocked"}
    <circle cx="12" cy="12" r="8.5" />
    <path d="M6.2 6.2 17.8 17.8" />
  {:else if name === "document"}
    <path d="M7 3.5h7l4 4V19a1.5 1.5 0 0 1-1.5 1.5H7A1.5 1.5 0 0 1 5.5 19V5A1.5 1.5 0 0 1 7 3.5Z" />
    <path d="M13.8 3.5V8h4.4" />
  {:else if name === "flame"}
    <path
      d="M12 3s5 3.6 5 8.5a5 5 0 0 1-10 0c0-1.6.7-2.9 1.5-3.8C9 9 10 10 10 10s-.5-3 2-7Z"
      fill={filled ? "currentColor" : "none"}
    />
  {:else if name === "graph"}
    <!-- knowledge-graph: linked nodes -->
    <circle cx="6" cy="7" r="2.4" fill={filled ? "currentColor" : "none"} />
    <circle cx="18" cy="6" r="2.4" fill={filled ? "currentColor" : "none"} />
    <circle cx="8" cy="18" r="2.4" fill={filled ? "currentColor" : "none"} />
    <circle cx="17.5" cy="17" r="2.4" fill={filled ? "currentColor" : "none"} />
    <path d="M7.7 8.7 6.3 15.6M8.1 6.6l7.6-.4M8.2 16.5l7-9.2M10.2 18.4l5.1-1" />
  {:else if name === "activity"}
    <!-- pulse / activity feed -->
    <path d="M3.5 12h3.5l2-6 4 13 2.5-7H21" />
  {:else}
    <circle cx="12" cy="12" r="2" fill="currentColor" stroke="none" />
  {/if}
</svg>

<style>
  .icon {
    flex: none;
    vertical-align: -0.15em;
    pointer-events: none;
  }
</style>
