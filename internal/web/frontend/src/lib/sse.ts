import type { QueryClient } from "@tanstack/svelte-query";
import { editorState, markChangedOnDisk } from "./editorState.svelte";

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
      const open = editorState.handle;
      if (open) {
        // An editor is open. Refetching its ["raw"] would replace the buffer's
        // captured etag with a newer one, so the next save would diverge from
        // (or 409 against) what the user is actually editing. Skip that one key
        // and flag the on-disk change so the editor can offer a merge (W1); the
        // rest of the store still refreshes.
        markChangedOnDisk();
        qc.invalidateQueries({
          predicate: (query) => {
            const k = query.queryKey;
            return !(Array.isArray(k) && k[0] === "raw" && k[1] === open);
          },
        });
      } else {
        qc.invalidateQueries(); // store changed elsewhere — refresh everything
      }
    }
  };
  return () => es.close();
}
