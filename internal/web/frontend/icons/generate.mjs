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
const any = readFileSync(join(here, "icon.svg"));
const maskable = readFileSync(join(here, "icon-maskable.svg"));

const jobs = [
  { src: any, size: 192, out: "pwa-192.png" },
  { src: any, size: 512, out: "pwa-512.png" },
  { src: maskable, size: 512, out: "pwa-maskable-512.png" },
  { src: maskable, size: 180, out: "apple-touch-icon.png" },
  { src: any, size: 32, out: "favicon-32.png" },
];

for (const j of jobs) {
  await sharp(j.src, { density: 384 }).resize(j.size, j.size).png().toFile(join(pub, j.out));
  console.log(`wrote public/${j.out} (${j.size}px)`);
}
