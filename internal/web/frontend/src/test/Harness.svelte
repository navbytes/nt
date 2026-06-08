<script lang="ts">
  import { QueryClientProvider, QueryClient } from "@tanstack/svelte-query";
  import type { Component } from "svelte";

  let { comp, props = {} }: { comp: Component<any>; props?: Record<string, unknown> } = $props();

  const Comp = comp;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
</script>

<QueryClientProvider {client}>
  <Comp {...props} />
</QueryClientProvider>
