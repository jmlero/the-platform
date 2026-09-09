<script lang="ts">
  import TaskCard from './TaskCard.svelte';
  import { workQueues } from '$lib/work-queues';
  import type { Agent, Task } from '$lib/types';

  export let tasks: Task[] = [];
  export let agents: Agent[] = [];
  export let onselect: (task: Task) => void;
  $: queues = workQueues(tasks);
  $: sections = [
    { title: 'Needs your attention', note: 'Decisions, failures, and evidence that needs checking.', tasks: queues.attention, empty: 'No work needs your attention.' },
    { title: 'In progress', note: 'Authorized work and results still being verified or synchronized.', tasks: queues.active, empty: 'No work is running or queued.' },
    { title: 'Results', note: 'Inspect the result and its evidence. Completion does not imply a merge or deployment.', tasks: queues.results, empty: 'Completed results will appear here.' }
  ];
</script>

<div class="overview">
  {#each sections as section (section.title)}
    <section aria-label={section.title}>
      <header><h2>{section.title}<span>{section.tasks.length}</span></h2><p>{section.note}</p></header>
      <div class="cards">
        {#each section.tasks as task (task.id)}
          <TaskCard {task} agent={agents.find(agent => agent.id === task.assigned_agent_id)} {onselect} />
        {:else}
          <p class="empty">{section.empty}</p>
        {/each}
      </div>
    </section>
  {/each}
</div>

<style>
  .overview { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 20px; align-items: start; }
  section { min-width: 0; border: 1px solid var(--line); border-radius: 14px; background: var(--panel); }
  header { padding: 20px; border-bottom: 1px solid var(--line); }
  h2 { display: flex; align-items: center; justify-content: space-between; margin: 0; font-size: 1rem; }
  h2 span { color: var(--acid); font-size: .85rem; }
  header p { min-height: 3em; margin: 10px 0 0; color: var(--muted); font-size: .8rem; line-height: 1.5; }
  .cards { display: grid; gap: 12px; padding: 14px; }
  .empty { padding: 24px 8px; text-align: center; color: var(--subtle); font-size: .8rem; }
  @media (max-width: 1100px) { .overview { grid-template-columns: 1fr; } header p { min-height: 0; } }
</style>
