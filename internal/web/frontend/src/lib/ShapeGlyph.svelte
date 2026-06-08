<script lang="ts">
  import { glyphPoints, type ShapeKind } from "./graphShapes";

  let { kind, size = 11 }: { kind: ShapeKind; size?: number } = $props();
</script>

<svg width={size} height={size} viewBox="0 0 12 12" aria-hidden="true">
  {#if kind === "circle"}
    <circle cx="6" cy="6" r="5" fill="currentColor" />
  {:else}
    <polygon points={glyphPoints(kind)} fill="currentColor" />
  {/if}
</svg>
