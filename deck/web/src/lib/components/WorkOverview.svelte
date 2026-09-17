<script lang="ts">
  import { CircleGauge, GitPullRequestArrow, Inbox } from '@lucide/svelte';
  import TaskCard from './TaskCard.svelte';
  import { workQueues } from '$lib/work-queues';
  import type { Agent, Task } from '$lib/types';

  export let tasks: Task[] = [];
  export let agents: Agent[] = [];
  export let onselect: (task: Task) => void;
  export let searching = false;
  $: queues = workQueues(tasks);
  $: sections = [
    { title: 'Needs your attention', tone: 'attention', icon: Inbox, note: 'Decisions and exceptions that need a closer look.', tasks: queues.attention, empty: 'You’re all caught up', detail: 'Decisions and exceptions will appear here.' },
    { title: 'In progress', tone: 'active', icon: CircleGauge, note: 'Authorized work, from the queue to verification.', tasks: queues.active, empty: 'Room for the next idea', detail: 'Work appears here when it’s ready to begin.' },
    { title: 'Results', tone: 'results', icon: GitPullRequestArrow, note: 'Review the outcome and its supporting evidence.', tasks: queues.results, empty: 'Good work lands here', detail: 'Completed results will be ready for your review.' }
  ];
</script>

<div class="overview">
  {#each sections as section (section.title)}
    <section class={section.tone} aria-label={section.title}>
      <header>
        <div class="section-title"><span class="section-dot"></span><h2>{section.title}</h2><span class="count">{section.tasks.length}</span></div>
        <p>{section.note}</p>
      </header>
      <div class="cards">
        {#each section.tasks as task (task.id)}
          <TaskCard {task} agent={agents.find(agent => agent.id === task.assigned_agent_id)} {onselect} />
        {:else}
          <div class="empty">
            <span class="empty-icon"><svelte:component this={section.icon} size={22} strokeWidth={1.5} /></span>
            <h3>{searching ? 'No matching work' : section.empty}</h3>
            <p>{searching ? 'Try a different search.' : section.detail}</p>
          </div>
        {/each}
      </div>
      {#if section.tone === 'results' && section.tasks.length}
        <p class="result-note">Completion is ready for review. Merge and deployment remain your decision.</p>
      {/if}
    </section>
  {/each}
</div>

<style>
  .overview { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 20px; align-items: start; flex-shrink: 0; padding-bottom: 12px; }
  section { --section-color: var(--accent); min-width: 0; }
  .attention { --section-color: var(--warning); }
  .results { --section-color: var(--success); }
  header { padding: 0 0 16px; }
  .section-title { display: flex; align-items: center; gap: 8px; }
  .section-dot { width: 7px; height: 7px; flex-shrink: 0; background: var(--section-color); border-radius: 50%; }
  h2 { margin: 0; font-size: var(--text-base); font-weight: 650; }
  .count { display: grid; min-width: 23px; height: 23px; margin-left: auto; color: var(--muted); background: var(--panel-soft); border-radius: 6px; font-size: var(--text-xs); font-variant-numeric: tabular-nums; place-items: center; }
  header p { min-height: 3em; margin: 8px 0 0; color: var(--subtle); font-size: var(--text-xs); line-height: 1.5; }
  .cards { display: grid; gap: 12px; }
  .empty { display: grid; min-height: 225px; padding: 28px 20px; text-align: center; justify-items: center; align-content: center; background: color-mix(in srgb, var(--panel) 60%, transparent); border: 1px dashed var(--line-strong); border-radius: var(--radius-md); }
  .empty-icon { display: grid; width: 48px; height: 48px; margin-bottom: 16px; color: var(--subtle); background: var(--panel-soft); border-radius: 50%; place-items: center; }
  .empty h3 { margin: 0 0 6px; font-size: var(--text-base); font-weight: 550; }
  .empty p { margin: 0; max-width: 220px; color: var(--subtle); font-size: var(--text-sm); }
  .result-note { margin: 12px 4px 0; color: var(--subtle); font-size: var(--text-xs); }
  @media (max-width: 760px) { .overview { grid-template-columns: 1fr; gap: 28px; } header p { min-height: 0; } .empty { min-height: 180px; } }
</style>
