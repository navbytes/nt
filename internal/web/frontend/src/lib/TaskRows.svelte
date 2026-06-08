<script lang="ts">
  import { createQuery, createMutation, useQueryClient } from "@tanstack/svelte-query";
  import { api, type TaskGroup } from "./api";

  let {
    canEdit = false,
    statuses = null,
    showAdd = false,
  }: { canEdit?: boolean; statuses?: string[] | null; showAdd?: boolean } = $props();

  const qc = useQueryClient();
  const tasksQ = createQuery({ queryKey: ["tasks"], queryFn: api.tasks });

  const set = (d: { groups: TaskGroup[] }) => qc.setQueryData(["tasks"], d);
  const doneMut = createMutation({ mutationFn: api.taskDone, onSuccess: set });
  const reopenMut = createMutation({ mutationFn: api.taskReopen, onSuccess: set });
  const addMut = createMutation({ mutationFn: api.taskNew, onSuccess: set });

  let newText = $state("");

  const groups = $derived(
    (($tasksQ.data?.groups ?? []) as TaskGroup[]).filter(
      (g) => !statuses || statuses.includes(g.status),
    ),
  );

  function add(e: SubmitEvent) {
    e.preventDefault();
    const t = newText.trim();
    if (t) {
      $addMut.mutate(t);
      newText = "";
    }
  }
</script>

{#if showAdd && canEdit}
  <form class="taskadd" onsubmit={add}>
    <input placeholder="Add a task…" bind:value={newText} autocomplete="off" />
    <button class="btn" type="submit">Add</button>
  </form>
{/if}

{#if $tasksQ.isPending}
  <p class="muted">Loading tasks…</p>
{:else if $tasksQ.error}
  <p class="error">{String($tasksQ.error)}</p>
{:else}
  {#each groups as group (group.status)}
    <section class="group">
      <h2 class="group__title">{group.status} · {group.tasks.length}</h2>
      <ul class="rows">
        {#each group.tasks as t (t.id)}
          <li class="row">
            {#if canEdit}
              {#if t.status === "done"}
                <button class="check check--done" title="Reopen" onclick={() => $reopenMut.mutate(t.id)}
                  >●</button
                >
              {:else}
                <button class="check" title="Mark done" onclick={() => $doneMut.mutate(t.id)}>○</button>
              {/if}
            {/if}
            <span class="row__text" class:done={t.status === "done"}>{t.text}</span>
            {#if t.project}<span class="chip">+{t.project}</span>{/if}
            {#if t.due}<span class="row__due">{t.due}</span>{/if}
            {#if t.source}<span class="src">{t.source}</span>{/if}
          </li>
        {/each}
      </ul>
      {#if group.tasks.length === 0}<p class="muted small">none</p>{/if}
    </section>
  {:else}
    <p class="muted">No tasks.</p>
  {/each}
{/if}
