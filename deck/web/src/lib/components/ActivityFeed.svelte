<script lang="ts">
  import {
    Bot,
    CheckCircle2,
    CircleDot,
    GitPullRequestArrow,
    ListRestart,
    Radio,
    ShieldCheck
  } from '@lucide/svelte';
  import { eventLabel, timeAgo } from '$lib/format';
  import type { Agent, AppEvent, Task } from '$lib/types';

  export let events: AppEvent[] = [];
  export let tasks: Task[] = [];
  export let agents: Agent[] = [];
  export let onselect: (task: Task) => void;

  function taskFor(event: AppEvent) {
    return tasks.find((task) => task.id === event.task_id);
  }

  function agentFor(event: AppEvent) {
    return agents.find((agent) => agent.id === event.agent_id);
  }

  function iconFor(type: string) {
    if (type.startsWith('decision.')) return GitPullRequestArrow;
    if (type === 'run.completed') return CheckCircle2;
    if (type === 'run.verifying') return ShieldCheck;
    if (type === 'run.lease_expired') return ListRestart;
    if (type.startsWith('run.')) return Radio;
    if (type.startsWith('agent.')) return Bot;
    return CircleDot;
  }
</script>

<aside class="activity surface">
  <header>
    <div>
      <span class="eyebrow">Event log</span>
      <h2>Live activity</h2>
    </div>
    <span class="live-dot" title="Receiving durable events"></span>
  </header>

  <div class="timeline">
    {#each events.slice(0, 30) as event (event.id)}
      {@const task = taskFor(event)}
      {@const agent = agentFor(event)}
      {@const Icon = iconFor(event.type)}
      <button
        type="button"
        class="event"
        class:decision={event.type.startsWith('decision.')}
        class:completed={event.type === 'run.completed'}
        on:click={() => task && onselect(task)}
        disabled={!task}
      >
        <span class="event-icon"><Icon size={14} strokeWidth={2} /></span>
        <span class="event-copy">
          <span class="event-meta">
            <strong>{agent?.name ?? eventLabel(event.type)}</strong>
            <time>{timeAgo(event.created_at)}</time>
          </span>
          <span class="event-message">{event.message}</span>
          {#if task}
            <span class="event-task">{task.title}</span>
          {/if}
        </span>
      </button>
    {:else}
      <div class="empty">
        <CircleDot size={17} />
        Activity will appear as work moves.
      </div>
    {/each}
  </div>
</aside>

<style>
  .activity {
    display: flex;
    width: 300px;
    min-width: 300px;
    flex-direction: column;
    overflow: hidden;
    border-radius: 16px;
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 17px 18px 14px;
    border-bottom: 1px solid var(--line);
  }

  h2 {
    margin: 4px 0 0;
    font-size: 0.9rem;
    font-weight: 680;
  }

  .live-dot {
    width: 8px;
    height: 8px;
    background: var(--accent);
    border-radius: 999px;
    box-shadow: 0 0 0 5px var(--accent-soft);
  }

  .timeline {
    min-height: 0;
    flex: 1;
    padding: 7px 8px 14px;
    overflow-y: auto;
  }

  .event {
    position: relative;
    display: flex;
    width: 100%;
    gap: 10px;
    padding: 10px;
    text-align: left;
    background: transparent;
    border: 0;
    border-radius: 10px;
  }

  .event:not(:disabled) {
    cursor: pointer;
  }

  .event:not(:disabled):hover {
    background: var(--panel-soft);
  }

  .event::after {
    position: absolute;
    top: 37px;
    bottom: -6px;
    left: 20px;
    width: 1px;
    background: var(--line);
    content: "";
  }

  .event:last-child::after {
    display: none;
  }

  .event-icon {
    z-index: 1;
    display: grid;
    width: 25px;
    height: 25px;
    flex: 0 0 auto;
    color: var(--muted);
    background: var(--panel-soft);
    border: 1px solid var(--line);
    border-radius: 8px;
    place-items: center;
  }

  .event.decision .event-icon {
    color: var(--warning);
    background: var(--warning-soft);
  }

  .event.completed .event-icon {
    color: var(--accent);
    background: var(--accent-soft);
  }

  .event-copy {
    display: block;
    min-width: 0;
    flex: 1;
  }

  .event-meta {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
  }

  .event-meta strong {
    overflow: hidden;
    font-size: var(--text-sm);
    font-weight: 700;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  time {
    flex: 0 0 auto;
    color: var(--subtle);
    font-size: var(--text-xs);
  }

  .event-message {
    display: block;
    margin-top: 3px;
    color: var(--muted);
    font-size: var(--text-sm);
    line-height: 1.45;
  }

  .event-task {
    display: block;
    margin-top: 4px;
    overflow: hidden;
    color: var(--subtle);
    font-size: var(--text-xs);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .empty {
    display: flex;
    min-height: 160px;
    align-items: center;
    justify-content: center;
    gap: 8px;
    color: var(--subtle);
    font-size: var(--text-sm);
  }

  @media (max-width: 1280px) {
    .activity {
      display: none;
    }
  }
</style>
