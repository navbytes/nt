// Rasterizes the source SVG marks into the PNG icons the PWA manifest + iOS need.
// Run with `npm run icons` (uses sharp). Outputs land in public/, which Vite
// copies to dist/ at the site root. The PNGs are committed so CI/`go build`
// (which never run this) still embed current icons.
import sharp from "sharp";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const pub = join(here, "..", "public");
// The desktop (Wails) shell lives at the repo root; its build picks up appicon.png
// and derives the platform icon set from it. Render it from the SAME brand mark so
// the desktop app stops shipping the default Wails "W" placeholder.
const desktopIcon = join(here, "..", "..", "..", "..", "desktop", "build", "appicon.png");
const any = readFileSync(join(here, "icon.svg"));
const maskable = readFileSync(join(here, "icon-maskable.svg"));

const jobs = [
  { src: any, size: 192, out: join(pub, "pwa-192.png"), label: "public/pwa-192.png" },
  { src: any, size: 512, out: join(pub, "pwa-512.png"), label: "public/pwa-512.png" },
  { src: maskable, size: 512, out: join(pub, "pwa-maskable-512.png"), label: "public/pwa-maskable-512.png" },
  { src: maskable, size: 180, out: join(pub, "apple-touch-icon.png"), label: "public/apple-touch-icon.png" },
  { src: any, size: 32, out: join(pub, "favicon-32.png"), label: "public/favicon-32.png" },
  { src: any, size: 1024, out: desktopIcon, label: "desktop/build/appicon.png" },
];

for (const j of jobs) {
  await sharp(j.src, { density: 384 }).resize(j.size, j.size).png().toFile(j.out);
  console.log(`wrote ${j.label} (${j.size}px)`);
}
