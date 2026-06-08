<script lang="ts">
  import { onMount } from "svelte";
  import { QueryClientProvider } from "@tanstack/svelte-query";
  import { queryClient } from "./lib/query";
  import { initRouter } from "./lib/router.svelte";
  import { startSSE } from "./lib/sse";
  import Shell from "./lib/Shell.svelte";

  onMount(() => {
    const stopRouter = initRouter();
    const stopSSE = startSSE(queryClient);
    return () => {
      stopRouter();
      stopSSE();
    };
  });
</script>

<QueryClientProvider client={queryClient}>
  <Shell />
</QueryClientProvider>
