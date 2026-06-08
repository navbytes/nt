<script lang="ts">
  let { path }: { path: string } = $props();
</script>

<h1>Not found</h1>
<p class="muted">Nothing at <code>{path}</code>.</p>
<p><a href="/">← Back to dashboard</a></p>
