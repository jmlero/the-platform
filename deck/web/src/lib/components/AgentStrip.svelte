<script lang="ts">
  import { Bot, Cpu, Radio } from '@lucide/svelte';
  import { initials, timeAgo } from '$lib/format';
  import type { Agent, Task } from '$lib/types';

  export let agents: Agent[] = [];
  export let tasks: Task[] = [];
  export let onselect: (task: Task) => void;

  function taskFor(agent: Agent): Task | undefined {
    return tasks.find((task) => task.assigned_agent_id === agent.id);
  }

  function effectiveStatus(agent: Agent): string {
    if (Date.now() - agent.last_seen_at > 30_000) return 'offline';
    return agent.status;
  }
</script>

<section class="agent-strip surface" aria-label="Connected agents">
  <div class="strip-label">
    <span class="icon"><Cpu size={15} /></span>
    <div>
      <span class="eyebrow">Connected agents</span>
      <strong>{agents.filter((agent) => effectiveStatus(agent) !== 'offline').length} connected</strong>
    </div>
  </div>

  <div class="agents">
    {#each agents as agent (agent.id)}
      {@const task = taskFor(agent)}
      {@const status = effectiveStatus(agent)}
      <button
        type="button"
        class="agent"
        class:active={status === 'active'}
        class:waiting={status === 'waiting'}
        class:offline={status === 'offline'}
        on:click={() => task && onselect(task)}
        disabled={!task}
      >
        <span class="agent-avatar">
          {#if status === 'active'}<i class="pulse"></i>{/if}
          {initials(agent.name)}
        </span>
        <span class="agent-copy">
          <span class="agent-name">
            {agent.name}
            <small>{status}</small>
          </span>
          <span class="agent-task clamp-2">
            {task?.title ?? (status === 'offline' ? `Last seen ${timeAgo(agent.last_seen_at)}` : 'Waiting for ready work')}
          </span>
        </span>
        {#if status === 'active'}
          <span class="radio"><Radio size={13} /></span>
        {/if}
      </button>
    {:else}
      <div class="no-agents">
        <Bot size={17} />
        No agents have connected yet.
      </div>
    {/each}
  </div>
</section>

<style>
  .agent-strip {
    display: flex;
    min-height: 86px;
    flex: 0 0 auto;
    align-items: stretch;
    padding: 11px;
    border-radius: 16px;
  }

  .strip-label {
    display: flex;
    width: 156px;
    flex: 0 0 auto;
    align-items: center;
    gap: 11px;
    padding: 0 12px 0 4px;
    border-right: 1px solid var(--line);
  }

  .strip-label .icon {
    display: grid;
    width: 32px;
    height: 32px;
    color: var(--accent);
    background: var(--accent-soft);
    border-radius: 10px;
    place-items: center;
  }

  .strip-label strong {
    display: block;
    margin-top: 3px;
    font-size: var(--text-sm);
    font-weight: 600;
  }

  .agents {
    display: flex;
    min-width: 0;
    align-items: stretch;
    gap: 8px;
    padding-left: 10px;
    overflow-x: auto;
  }

  .agent {
    position: relative;
    display: flex;
    width: 235px;
    flex: 0 0 auto;
    align-items: center;
    gap: 10px;
    padding: 9px 10px;
    text-align: left;
    background: var(--panel-soft);
    border: 1px solid transparent;
    border-radius: 11px;
    transition: 150ms ease;
  }

  .agent:not(:disabled) {
    cursor: pointer;
  }

  .agent:not(:disabled):hover {
    background: var(--panel-soft);
    border-color: var(--line);
  }

  .agent:disabled {
    opacity: 1;
  }

  .agent-avatar {
    position: relative;
    display: grid;
    width: 39px;
    height: 39px;
    flex: 0 0 auto;
    color: var(--on-accent);
    font-size: var(--text-sm);
    font-weight: 850;
    background: var(--accent);
    border-radius: 12px;
    place-items: center;
  }

  .agent:not(.active) .agent-avatar {
    color: var(--muted);
    background: var(--panel-soft);
  }

  .agent.waiting .agent-avatar {
    color: var(--warning);
    background: var(--warning-soft);
  }

  .agent.offline {
    opacity: 1;
  }

  .pulse {
    position: absolute;
    inset: -4px;
    border: 1px solid var(--accent);
    border-radius: 15px;
    animation: pulse-ring 1.8s ease-out infinite;
  }

  .agent-copy {
    min-width: 0;
  }

  .agent-name {
    display: flex;
    align-items: center;
    gap: 7px;
    font-size: var(--text-sm);
    font-weight: 700;
  }

  .agent-name small {
    color: var(--accent);
    font-size: var(--text-xs);
    font-weight: 750;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .waiting .agent-name small {
    color: var(--warning);
  }

  .offline .agent-name small {
    color: var(--subtle);
  }

  .agent-task {
    display: block;
    margin-top: 4px;
    color: var(--muted);
    font-size: var(--text-sm);
    line-height: 1.35;
  }

  .radio {
    position: absolute;
    top: 8px;
    right: 8px;
    color: var(--accent);
  }

  .no-agents {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 12px;
    color: var(--muted);
    font-size: var(--text-sm);
  }

  @media (max-width: 800px) {
    .strip-label {
      width: 118px;
    }

    .agent {
      width: 210px;
    }
  }
</style>
