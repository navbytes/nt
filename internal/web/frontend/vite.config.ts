import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// The SPA is built to ./dist and embedded into the Go binary (see embed.go).
// In dev, `vite` proxies the API + live-update stream to a running `nt web`
// (default port 8765 — start it with `nt web --edit --port 8765`).
const API = process.env.NT_API ?? "http://127.0.0.1:8765";

export default defineConfig({
  plugins: [
    svelte({
      onwarn(warning, handler) {
        // We pass route params (handle, q) into createQuery options and remount
        // the route via {#key …} when they change, so capturing the initial
        // value per mount is intentional — not the bug this warning guards.
        if (warning.code === "state_referenced_locally") return;
        handler?.(warning);
      },
    }),
  ],
  // Assets are served from /assets by the Go binary; absolute base keeps URLs
  // stable regardless of the SPA route.
  base: "/",
  build: {
    outDir: "dist",
    emptyOutDir: true,
    target: "es2022",
  },
  server: {
    port: 5173,
    proxy: {
      "/api": { target: API, changeOrigin: true },
      "/events": { target: API, changeOrigin: true },
      "/static": { target: API, changeOrigin: true },
      "/n": { target: API, changeOrigin: true },
    },
  },
});
