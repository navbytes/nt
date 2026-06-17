// Minimal client-side router. TanStack Router has no Svelte adapter, and our
// route set is small and fixed, so a runes-based path router is the robust,
// zero-dependency choice. The Go server already serves index.html for unknown
// paths (SPA fallback), so deep links and reloads work. Data caching is handled
// by TanStack Query, not router loaders — so we lose nothing by going minimal.

export const loc = $state({
  path: window.location.pathname,
  query: new URLSearchParams(window.location.search),
});

function sync(): void {
  loc.path = window.location.pathname;
  loc.query = new URLSearchParams(window.location.search);
}

// SPA leave guards (W2): the open editor registers a guard that confirms before
// navigating away with unsaved edits. Kept as a generic list so other views can
// opt in later without coupling the router to the editor.
type LeaveGuard = () => boolean; // return false to block navigation
const leaveGuards = new Set<LeaveGuard>();
export function registerLeaveGuard(g: LeaveGuard): () => void {
  leaveGuards.add(g);
  return () => leaveGuards.delete(g);
}
function canLeave(): boolean {
  for (const g of leaveGuards) if (!g()) return false;
  return true;
}

export function navigate(to: string): void {
  if (to === loc.path + (loc.query.toString() ? `?${loc.query}` : "")) return;
  if (!canLeave()) return;
  history.pushState({}, "", to);
  sync();
  window.scrollTo(0, 0);
}

// Intercept same-origin left-clicks on <a href="/…"> so navigation stays in the
// SPA; modified clicks, new-tab, downloads, and external links fall through.
function onClick(e: MouseEvent): void {
  if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey)
    return;
  const a = (e.target as HTMLElement).closest("a");
  if (!a) return;
  const href = a.getAttribute("href");
  if (
    !href ||
    !href.startsWith("/") ||
    a.target === "_blank" ||
    a.hasAttribute("download") ||
    a.getAttribute("rel") === "external"
  )
    return;
  e.preventDefault();
  navigate(href);
}

export function initRouter(): () => void {
  window.addEventListener("popstate", sync);
  document.addEventListener("click", onClick);
  return () => {
    window.removeEventListener("popstate", sync);
    document.removeEventListener("click", onClick);
  };
}
