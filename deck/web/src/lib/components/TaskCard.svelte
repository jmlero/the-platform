<script lang="ts">
  import { Bot, Clock3, CheckCircle2, GitPullRequestArrow, ShieldCheck } from '@lucide/svelte';
  import { initials, timeAgo } from '$lib/format';
  import type { Agent, Task } from '$lib/types';

  export let task: Task;
  export let agent: Agent | undefined = undefined;
  export let draggable = false;
  export let onselect: (task: Task) => void;
  export let ondrag: (event: DragEvent, task: Task) => void = () => undefined;

  const priorityClass: Record<Task['priority'], string> = {
    low: 'priority-low',
    medium: 'priority-medium',
    high: 'priority-high',
    urgent: 'priority-urgent'
  };

  $: displayState = task.display_state ?? (task.status === 'done' ? 'completed' : task.status === 'waiting_decision' ? 'waiting' : task.status.replace('_', '-'));

</script>

<button
  type="button"
  class="task-card"
  class:is-waiting={displayState === 'waiting'}
  class:is-running={displayState === 'running'}
  class:is-done={displayState === 'completed'}
  class:is-attention={displayState === 'blocked' || displayState === 'stale-evidence' || task.status === 'failed'}
  {draggable}
  on:click={() => onselect(task)}
  on:dragstart={(event) => ondrag(event, task)}
>
  <div class="card-topline">
    <span class="priority-label {priorityClass[task.priority]}">
      <span class="priority-dot"></span>
      {task.priority}
    </span>
    <span class="task-age">{timeAgo(task.updated_at)}</span>
  </div>

  <h3>{task.title}</h3>
  {#if task.description}
    <p class="clamp-2">{task.description}</p>
  {/if}

  {#if displayState === 'waiting'}
    <div class="signal signal-decision">
      <GitPullRequestArrow size={13} strokeWidth={2.2} />
      Human decision needed
    </div>
  {:else if displayState === 'stale-evidence'}
    <div class="signal signal-attention"><ShieldCheck size={13} /> Evidence is stale</div>
  {:else if displayState === 'syncing'}
    <div class="signal signal-decision"><span class="mini-wave"><i></i><i></i><i></i></span> Synchronizing provider state</div>
  {:else if displayState === 'retrying'}
    <div class="signal signal-decision"><span class="mini-wave"><i></i><i></i><i></i></span> Retrying bounded work</div>
  {:else if displayState === 'verifying'}
    <div class="signal signal-verify">
      <ShieldCheck size={13} strokeWidth={2.2} />
      Checking acceptance
    </div>
  {:else if task.status === 'running'}
    <div class="signal signal-running">
      <span class="mini-wave"><i></i><i></i><i></i></span>
      Agent executing
    </div>
  {:else if task.status === 'done'}
    <div class="signal signal-done"><CheckCircle2 size={13} /> Ready for review</div>
  {:else if task.status === 'blocked' || task.status === 'failed'}
    <div class="signal signal-attention"><ShieldCheck size={13} /> {task.status === 'blocked' ? 'Work is blocked' : 'Run failed'}</div>
  {/if}

  <footer>
    <div class="assignee">
      {#if agent}
        <span class="avatar" title={agent.name}>{initials(agent.name)}</span>
        <span>{agent.name}</span>
      {:else}
        <span class="avatar avatar-empty"><Bot size={13} /></span>
        <span>Unassigned</span>
      {/if}
    </div>
    <div class="budget" title={`${task.budget_minutes} minute budget`}>
      <Clock3 size={13} />
      {task.budget_minutes}m
    </div>
  </footer>

</button>

<style>
  .task-card {
    position: relative;
    width: 100%;
    padding: 18px;
    flex-shrink: 0;
    overflow: hidden;
    background: var(--panel-raised);
    border: 1px solid var(--line);
    border-radius: var(--radius-md);
    cursor: pointer;
    color: inherit;
    text-align: left;
    box-shadow: var(--shadow-card);
    transition:
      border-color 150ms ease,
      transform 150ms ease,
      background 150ms ease;
  }

  .task-card:hover {
    background: var(--surface-hover);
    border-color: var(--line-strong);
    transform: translateY(-1px);
  }

  .task-card[draggable='true'] {
    cursor: grab;
  }

  .task-card[draggable='true']:active {
    cursor: grabbing;
  }

  .task-card.is-waiting {
    background: var(--panel);
    border-left: 3px solid color-mix(in srgb, var(--warning) 65%, var(--panel));
  }

  .task-card.is-running {
    border-left: 3px solid var(--accent);
  }

  .task-card.is-attention {
    border-left: 3px solid var(--danger);
  }

  .task-card.is-done {
    border-left: 3px solid var(--success);
  }

  .card-topline,
  footer,
  .assignee,
  .budget,
  .priority-label,
  .signal {
    display: flex;
    align-items: center;
  }

  .card-topline,
  footer {
    justify-content: space-between;
  }

  .priority-label {
    gap: 6px;
    font-size: .6875rem;
    font-weight: 600;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .priority-dot {
    width: 6px;
    height: 6px;
    background: currentColor;
    border-radius: 999px;
  }

  .priority-low {
    color: var(--subtle);
  }

  .priority-medium {
    color: var(--info);
  }

  .priority-high {
    color: var(--planning);
  }

  .priority-urgent {
    color: var(--danger);
  }

  .task-age {
    color: var(--subtle);
    font-size: var(--text-sm);
  }

  h3 {
    overflow-wrap: anywhere;
    margin: 13px 0 7px;
    color: var(--text);
    font-size: .9375rem;
    font-weight: 650;
    letter-spacing: -0.012em;
    line-height: 1.45;
  }

  p {
    min-height: 2.7em;
    margin: 0 0 13px;
    color: var(--muted);
    font-size: var(--text-sm);
    line-height: 1.55;
  }

  .signal {
    width: fit-content;
    gap: 6px;
    margin: 10px 0 12px;
    padding: 5px 8px;
    border-radius: 7px;
    font-size: var(--text-sm);
    font-weight: 650;
  }

  .signal-decision {
    color: var(--warning);
    background: var(--warning-soft);
  }

  .signal-running {
    color: var(--accent);
    background: var(--accent-soft);
  }

  .signal-verify {
    color: var(--info);
    background: var(--info-soft);
  }

  .signal-done { color: var(--success); background: var(--success-soft); }

  .signal-attention {
    color: var(--danger);
    background: var(--danger-soft);
  }

  footer {
    padding-top: 12px;
    border-top: 1px solid var(--line);
    color: var(--muted);
    font-size: var(--text-sm);
  }

  .assignee,
  .budget {
    gap: 7px;
  }

  .avatar {
    display: grid;
    width: 23px;
    height: 23px;
    color: var(--on-accent);
    font-size: var(--text-xs);
    font-weight: 850;
    background: var(--accent);
    border-radius: 7px;
    place-items: center;
  }

  .avatar-empty {
    color: var(--subtle);
    background: var(--panel-soft);
  }

  .mini-wave {
    display: flex;
    height: 10px;
    align-items: flex-end;
    gap: 2px;
  }

  .mini-wave i {
    width: 2px;
    height: 5px;
    background: currentColor;
    border-radius: 2px;
    animation: wave 800ms ease-in-out infinite alternate;
  }

  .mini-wave i:nth-child(2) {
    height: 9px;
    animation-delay: 120ms;
  }

  .mini-wave i:nth-child(3) {
    height: 7px;
    animation-delay: 240ms;
  }

  @keyframes wave {
    to {
      height: 3px;
    }
  }
</style>
