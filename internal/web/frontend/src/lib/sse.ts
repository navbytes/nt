import type { QueryClient } from "@tanstack/svelte-query";

// Bridges the server's existing SSE stream (/events) to TanStack Query cache
// invalidation — the same live-update channel the htmx UI uses. The server
// sends `tasks` for a task write and `reload` for any store change it didn't
// originate. EventSource auto-reconnects, so a dropped connection self-heals.
export function startSSE(qc: QueryClient): () => void {
  const es = new EventSource("/events");
  es.onmessage = (e) => {
    if (e.data === "tasks") {
      qc.invalidateQueries({ queryKey: ["tasks"] });
      qc.invalidateQueries({ queryKey: ["activity"] });
      qc.invalidateQueries({ queryKey: ["state"] });
    } else if (e.data === "reload") {
      qc.invalidateQueries(); // store changed elsewhere — refresh everything
    }
  };
  return () => es.close();
}
