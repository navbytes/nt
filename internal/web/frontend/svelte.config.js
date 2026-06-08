import { vitePreprocess } from "@sveltejs/vite-plugin-svelte";

export default {
  preprocess: vitePreprocess(),
  // Runes mode auto-enables per-component when a rune is used; we don't force it
  // globally so third-party legacy components (e.g. @tanstack/svelte-query) still
  // compile. Our components all use runes, so they opt in automatically.
};
