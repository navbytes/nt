<script lang="ts">
  import { onMount } from "svelte";
  import { createQuery } from "@tanstack/svelte-query";
  import { api } from "../lib/api";
  import { navigate } from "../lib/router.svelte";
  import { toForceGraph, type FGNode, type FGLink } from "../lib/graph";

  const graphQ = createQuery({ queryKey: ["graph"], queryFn: api.graph });

  let container: HTMLDivElement | undefined = $state();
  // force-graph's chainable instance is loosely typed here; the data flowing in
  // is strongly typed via toForceGraph(). It's a class instance, so $state keeps
  // it as-is (no proxy) and re-runs the data effect once it's constructed.
  let graph = $state<any>(null);

  onMount(() => {
    let destroyed = false;
    let ro: ResizeObserver | undefined;

    // Lazy-load: force-graph (+ d3-force) is a separate chunk, fetched only here.
    void (async () => {
      const { default: ForceGraph } = await import("force-graph");
      if (destroyed || !container) return;
      graph = new ForceGraph<FGNode, FGLink>(container)
        .nodeId("id")
        .nodeLabel("title")
        .nodeVal((n: FGNode) => 1 + n.deg)
        .nodeAutoColorBy("folder")
        .linkColor(() => "rgba(128,128,128,0.28)")
        .backgroundColor("rgba(0,0,0,0)")
        .onNodeClick((n: FGNode) => {
          if (n.url) navigate(n.url);
        });
      size();
      ro = new ResizeObserver(size);
      ro.observe(container);
    })();

    return () => {
      destroyed = true;
      ro?.disconnect();
      graph?._destructor?.();
      graph = null;
    };
  });

  function size() {
    if (graph && container) graph.width(container.clientWidth).height(container.clientHeight);
  }

  // Feed data once both the renderer and the query are ready.
  $effect(() => {
    const data = $graphQ.data;
    if (graph && data) graph.graphData(toForceGraph(data));
  });
</script>

<div class="pagehead">
  <h1>Graph</h1>
  {#if $graphQ.data}
    <span class="muted small">{$graphQ.data.nodes.length} notes · {$graphQ.data.links.length} links</span>
  {/if}
</div>

{#if $graphQ.error}
  <p class="error">Couldn't load the graph.</p>
{/if}

<div class="graph" bind:this={container}></div>
