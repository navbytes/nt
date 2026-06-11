// Shared Mermaid runner for server-rendered note HTML (the note page and the
// editor's live preview). The server's goldmark pipeline turns ```mermaid```
// fences into <div class="mermaid"> blocks; this renders them client-side in
// the active theme.

// isDarkTheme mirrors the CSS: an explicit data-theme wins, otherwise the OS
// preference decides (index.html only sets the attribute when one was saved).
function isDarkTheme(): boolean {
  const t = document.documentElement.getAttribute("data-theme");
  if (t) return t === "dark";
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

// renderMermaidIn renders every .mermaid block inside el. Each diagram's source
// is cached on first run (mermaid.run replaces the div's text with SVG) and
// restored before re-running, so a theme toggle re-renders from source in the
// new theme. No-op when el has no diagrams — mermaid is only imported on demand.
export async function renderMermaidIn(el: HTMLElement): Promise<void> {
  const divs = Array.from(el.querySelectorAll<HTMLElement>(".mermaid"));
  if (!divs.length) return;
  const mermaid = (await import("mermaid")).default;
  mermaid.initialize({ startOnLoad: false, theme: isDarkTheme() ? "dark" : "default" });
  for (const d of divs) {
    const src = d.getAttribute("data-src") ?? d.textContent ?? "";
    d.setAttribute("data-src", src);
    d.removeAttribute("data-processed");
    d.innerHTML = src;
  }
  await mermaid.run({ nodes: divs });
}

// observeTheme invokes cb whenever the document theme changes (the toggle flips
// data-theme). Returns a disconnect function for the caller's effect cleanup.
export function observeTheme(cb: () => void): () => void {
  const obs = new MutationObserver(cb);
  obs.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme", "class"] });
  return () => obs.disconnect();
}
