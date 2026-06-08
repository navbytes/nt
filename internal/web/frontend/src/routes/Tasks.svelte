<script lang="ts">
  import TaskRows from "../lib/TaskRows.svelte";

  let { canEdit }: { canEdit: boolean } = $props();
</script>

<h1>Tasks</h1>
<TaskRows {canEdit} showAdd={true} />
