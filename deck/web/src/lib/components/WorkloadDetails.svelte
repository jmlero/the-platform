<script lang="ts">
  import type { Task, AppEvent } from '$lib/types';
  export let task: Task;
  export let events: AppEvent[] = [];
  export let runID: string | undefined = undefined;
  $: current = events.filter((event) => event.run_id === runID).sort((a,b) => b.id-a.id);
  $: identity = current.find((event) => typeof event.payload?.workload === 'string')?.payload;
  $: workload = task.workload?.id ?? String(identity?.workload ?? '');
  $: profile = task.workload?.digest ?? String(identity?.profile_digest ?? '');
  $: harness = task.workload?.harness ?? String(identity?.harness ?? '');
  $: usage = current.find((event) => typeof event.payload?.reported_tokens === 'number')?.payload;
  $: progress = current.find((event) => event.type === 'execution.progress' || event.type === 'run.blocked' || event.type === 'model.budget_exhausted');
  $: pending = Number(usage?.pending_model_calls ?? 0);
</script>

{#if workload || usage}
  <section aria-label="Workload execution" class="workload-details">
    <strong>{workload === 'diagnostic-analysis/v1' ? 'Diagnostic analysis' : workload === 'repository/v1' ? 'Repository change' : 'Execution'}</strong>
    {#if harness}<p>{harness}</p>{/if}
    {#if progress}<p>{progress.message}</p>{/if}
    {#if usage}
      <p>Reported model usage: {Number(usage.reported_tokens).toLocaleString('en-US')} tokens · {(Number(usage.reported_cost_micros ?? 0) / 1000000).toFixed(4)} {String(usage.currency ?? '')}</p>
      {#if pending > 0}<p class="uncertain">Usage awaiting confirmation: {pending} model call(s). Reserved {Number(usage.reserved_tokens ?? 0).toLocaleString('en-US')} tokens · {(Number(usage.reserved_cost_micros ?? 0) / 1000000).toFixed(4)} {String(usage.currency ?? '')}.</p>{/if}
    {:else}<p>No usage reported yet.</p>{/if}
    <details><summary>Execution identity</summary><dl>
      {#if profile}<div><dt>Profile digest</dt><dd>{profile}</dd></div>{/if}
      {#if runID}<div><dt>Run</dt><dd>{runID}</dd></div>{/if}
      {#if identity?.execution_id}<div><dt>Execution</dt><dd>{String(identity.execution_id)}</dd></div>{/if}
    </dl></details>
  </section>
{/if}
<style>
.workload-details {margin: 0 24px 18px; padding: 14px; border: 1px solid var(--line); border-radius: 10px; background: var(--panel-soft); font-size: var(--text-sm);}
p {margin: 6px 0; color: var(--muted); line-height: 1.5; overflow-wrap: anywhere;}
.uncertain {color: var(--warning);}
summary {cursor: pointer; padding: 8px 0; color: var(--subtle);}
dl {margin: 0;} dl div {margin-top: 8px;} dt {color: var(--subtle);} dd {margin: 3px 0 0; overflow-wrap: anywhere; font-family: monospace; font-size: var(--text-xs);}
</style>
