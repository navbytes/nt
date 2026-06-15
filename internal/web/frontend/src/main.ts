import { mount } from "svelte";
import App from "./App.svelte";
import "./app.css";

// Mark the native desktop shell so the macOS window-chrome CSS ([data-desktop])
// applies — the sidebar insets below the traffic lights and the chrome becomes
// window-draggable. The pre-paint check in index.html catches the common case;
// this re-checks here because Wails injects `window.runtime`/`window.go` after
// the head script runs, so this is the reliable signal in the webview.
function markDesktopShell() {
  const w = window as unknown as { runtime?: unknown; go?: unknown };
  const isDesktop = !!w.runtime || !!w.go || /wails/i.test(navigator.userAgent);
  if (isDesktop) document.documentElement.setAttribute("data-desktop", "");
}
markDesktopShell();
// Wails can finish injecting its runtime a tick after module eval; re-check once.
setTimeout(markDesktopShell, 0);

const target = document.getElementById("app");
if (!target) throw new Error("missing #app mount point");

const app = mount(App, { target });
export default app;
