<script lang="ts">
  import TaskCard from './TaskCard.svelte';
  import { workQueues } from '$lib/work-queues';
  import type { Agent, Task } from '$lib/types';

  export let tasks: Task[] = [];
  export let agents: Agent[] = [];
  export let onselect: (task: Task) => void;
  export let searching = false;
  $: queues = workQueues(tasks);
  $: sections = [
    { title: 'Needs attention', tone: 'attention', tasks: queues.attention, empty: 'Nothing needs attention' },
    { title: 'In progress', tone: 'active', tasks: queues.active, empty: 'No active work' },
    { title: 'Results', tone: 'results', tasks: queues.results, empty: 'No results yet' }
  ];
</script>

<div class="overview">
  {#each sections as section (section.title)}
    <section class={section.tone} aria-label={section.title}>
      <header>
        <div class="section-title"><span class="section-dot"></span><h2>{section.title}</h2><span class="count">{section.tasks.length}</span></div>
      </header>
      <div class="cards">
        {#each section.tasks as task (task.id)}
          <TaskCard {task} agent={agents.find(agent => agent.id === task.assigned_agent_id)} {onselect} />
        {:else}
          <p class="empty">{searching ? 'No matching work' : section.empty}</p>
        {/each}
      </div>
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
  .count { display: grid; min-width: 23px; height: 23px; color: var(--muted); background: var(--panel-soft); border-radius: 6px; font-size: var(--text-xs); font-variant-numeric: tabular-nums; place-items: center; }
  .cards { display: grid; gap: 12px; }
  .empty { margin: 0; padding: 24px 0; color: var(--subtle); font-size: var(--text-sm); border-top: 1px solid var(--line); }
  @media (max-width: 760px) { .overview { grid-template-columns: 1fr; gap: 28px; } }
</style>
