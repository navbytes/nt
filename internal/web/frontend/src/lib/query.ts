import { QueryClient } from "@tanstack/svelte-query";

// Single client for the app. Notes/tasks/etc. are pushed live via SSE
// (see sse.ts → invalidateQueries), so we don't need aggressive refetch
// intervals; a modest stale time avoids redundant fetches during navigation.
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
});
