import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { VitePWA } from "vite-plugin-pwa";

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
    // PWA: installable app shell + offline-capable static assets. The app's data
    // is live (SSE-driven), so the service worker precaches only the build shell
    // and never the API — navigations fall back to index.html, but /api, /events,
    // /static, and /n note bodies always hit the network.
    VitePWA({
      registerType: "autoUpdate",
      includeAssets: ["favicon.svg", "favicon-32.png", "apple-touch-icon.png"],
      manifest: {
        name: "nt — notes & tasks",
        short_name: "nt",
        description: "Durable notes & tasks — the memory layer for AI coding sessions.",
        theme_color: "#16161e",
        background_color: "#16161e",
        display: "standalone",
        start_url: "/",
        scope: "/",
        icons: [
          { src: "pwa-192.png", sizes: "192x192", type: "image/png" },
          { src: "pwa-512.png", sizes: "512x512", type: "image/png" },
          { src: "pwa-maskable-512.png", sizes: "512x512", type: "image/png", purpose: "maskable" },
        ],
      },
      workbox: {
        globPatterns: ["**/*.{js,css,html,svg,png,woff2}"],
        navigateFallback: "/index.html",
        // These are server-owned or live endpoints — never serve the SPA shell
        // (or a cached copy) for them.
        navigateFallbackDenylist: [/^\/api/, /^\/events/, /^\/static/],
        // Don't let the precache balloon on the large lazy mermaid/graph chunks.
        maximumFileSizeToCacheInBytes: 4 * 1024 * 1024,
      },
    }),
  ],
  resolve: {
    // The 3D graph (3d-force-graph) and the UnrealBloomPass we import for its
    // glow must resolve to ONE three instance, or the bloom pass won't attach.
    dedupe: ["three"],
  },
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
