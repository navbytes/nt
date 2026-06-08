import { defineConfig } from "vitest/config";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// Component tests run the real Svelte 5 components (runes + TanStack Query) in
// jsdom with the API client mocked — verifying rendering without a browser
// (Playwright's browser CDN is blocked in CI here). e2e is layered on top when a
// browser is available.
export default defineConfig({
  plugins: [
    svelte({
      hot: false,
      onwarn(warning, handler) {
        if (warning.code === "state_referenced_locally") return;
        handler?.(warning);
      },
    }),
  ],
  resolve: { conditions: ["browser"] },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.ts"],
  },
});
