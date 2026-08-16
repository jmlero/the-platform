<script lang="ts">
  import { Bot, Clock3, CornerDownRight, GitPullRequestArrow, ShieldCheck } from '@lucide/svelte';
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

  $: displayState = task.display_state ?? (task.status === 'done' ? 'completed' : task.status.replace('_', '-'));

</script>

<button
  type="button"
  class="task-card"
  class:is-waiting={task.status === 'waiting_decision'}
  class:is-running={task.status === 'running'}
  class:is-done={task.status === 'done'}
  class:is-attention={task.status === 'blocked' || task.status === 'failed'}
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

  {#if task.status === 'done'}
    <span class="done-mark"><CornerDownRight size={14} /></span>
  {/if}
</button>

<style>
  .task-card {
    position: relative;
    width: 100%;
    padding: 15px;
    overflow: hidden;
    background: var(--panel-raised);
    border: 1px solid var(--line);
    border-radius: 13px;
    cursor: pointer;
    color: inherit;
    text-align: left;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.13);
    transition:
      border-color 150ms ease,
      transform 150ms ease,
      background 150ms ease;
  }

  .task-card:hover {
    background: #191e24;
    border-color: var(--line-strong);
    transform: translateY(-2px);
  }

  .task-card[draggable='true'] {
    cursor: grab;
  }

  .task-card[draggable='true']:active {
    cursor: grabbing;
  }

  .task-card.is-waiting {
    background: linear-gradient(145deg, rgba(243, 184, 92, 0.085), var(--panel-raised) 55%);
    border-color: rgba(243, 184, 92, 0.22);
  }

  .task-card.is-running {
    border-color: rgba(184, 243, 107, 0.19);
  }

  .task-card.is-attention {
    border-color: rgba(255, 120, 111, 0.22);
  }

  .task-card.is-done {
    opacity: 0.72;
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
    font-size: 0.64rem;
    font-weight: 750;
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
    color: var(--cyan);
  }

  .priority-high {
    color: var(--violet);
  }

  .priority-urgent {
    color: var(--red);
  }

  .task-age {
    color: var(--subtle);
    font-size: 0.68rem;
  }

  h3 {
    margin: 13px 0 7px;
    color: var(--text);
    font-size: 0.91rem;
    font-weight: 650;
    letter-spacing: -0.012em;
    line-height: 1.35;
  }

  p {
    min-height: 2.7em;
    margin: 0 0 13px;
    color: var(--muted);
    font-size: 0.76rem;
    line-height: 1.55;
  }

  .signal {
    width: fit-content;
    gap: 6px;
    margin: 10px 0 12px;
    padding: 5px 8px;
    border-radius: 7px;
    font-size: 0.67rem;
    font-weight: 650;
  }

  .signal-decision {
    color: var(--amber);
    background: var(--amber-soft);
  }

  .signal-running {
    color: var(--acid);
    background: var(--acid-soft);
  }

  .signal-verify {
    color: var(--cyan);
    background: var(--cyan-soft);
  }

  .signal-attention {
    color: var(--red);
    background: var(--red-soft);
  }

  footer {
    padding-top: 12px;
    border-top: 1px solid var(--line);
    color: var(--muted);
    font-size: 0.68rem;
  }

  .assignee,
  .budget {
    gap: 7px;
  }

  .avatar {
    display: grid;
    width: 23px;
    height: 23px;
    color: #0d1209;
    font-size: 0.57rem;
    font-weight: 850;
    background: var(--acid);
    border-radius: 7px;
    place-items: center;
  }

  .avatar-empty {
    color: var(--subtle);
    background: rgba(255, 255, 255, 0.055);
  }

  .done-mark {
    position: absolute;
    top: -1px;
    right: -1px;
    display: grid;
    width: 31px;
    height: 31px;
    color: var(--acid);
    background: var(--acid-soft);
    border-bottom-left-radius: 12px;
    place-items: center;
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
