import { vitePreprocess } from "@sveltejs/vite-plugin-svelte";

export default {
  preprocess: vitePreprocess(),
  // Force Svelte 5 runes mode project-wide so $state/$derived are unambiguous.
  compilerOptions: { runes: true },
};
