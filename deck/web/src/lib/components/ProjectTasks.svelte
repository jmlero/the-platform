<script lang="ts">
 import { Plus, ArrowUpRight, GitBranch } from '@lucide/svelte';
 import { taskStages, taskStage, taskLabel } from '$lib/project-work';
 import type { Task } from '$lib/types';
 export let tasks:Task[]=[];
 export let search='';
 export let onselect:(task:Task)=>Promise<void>;
 export let oncreate:()=>void;
 $: filtered=tasks.filter(t=>`${t.title} ${t.description}`.toLowerCase().includes(search.toLowerCase()));
</script>

<section class="project-tasks" aria-label="Project tasks">
 <header><p>Discuss, refine, then move work to Ready.</p><button type="button" on:click={oncreate}><Plus size={15}/>Add task</button></header>
 {#each taskStages as stage}
  {@const items=filtered.filter(t=>taskStage(t)===stage.id)}
  <section class="group" aria-label={stage.label}>
   <h2>{stage.label}<span>{items.length}</span></h2>
   {#each items as task (task.id)}
    <button class="task-row" type="button" on:click={()=>onselect(task)}>
     <span class="task-name"><strong>{task.title}</strong>{#if task.depends_on?.length}<small><GitBranch size={12}/>{task.depends_on.length} prerequisite{task.depends_on.length===1?'':'s'}</small>{/if}</span>
     <span class:attention={['blocked','failed','waiting_decision'].includes(task.status)} class="state">{taskLabel(task)}</span><ArrowUpRight size={14}/>
    </button>
   {:else}<p class="empty">{search ? 'No matching tasks' : 'No tasks'}</p>{/each}
  </section>
 {/each}
</section>

<style>
 .project-tasks {max-width:1100px;width:100%;margin:0 auto;padding:24px 32px;}
 header {display:flex;align-items:center;justify-content:space-between;gap:16px;margin-bottom:24px;}
 header p,.empty {color:var(--subtle);font-size:13px;}
 button {cursor:pointer;color:var(--text);font:inherit;}
 header button {display:flex;align-items:center;gap:7px;border:1px solid var(--line);background:var(--panel);border-radius:6px;padding:9px 12px;font-size:13px;}
 .group {margin-bottom:28px;}
 h2 {display:flex;gap:10px;align-items:center;font-size:13px;font-weight:600;margin:0;padding:10px 0;border-bottom:1px solid var(--line);}
 h2 span {font-size:12px;color:var(--subtle);font-weight:400;}
 .task-row {display:flex;align-items:center;gap:16px;width:100%;text-align:left;background:transparent;border:0;border-bottom:1px solid var(--line);padding:16px 4px;}
 .task-row:hover {background:var(--surface-hover);}
 .task-name {flex:1;min-width:0;overflow-wrap:anywhere;}
 strong {font-size:14px;font-weight:500;} small {display:flex;gap:5px;align-items:center;color:var(--subtle);font-size:12px;margin-top:6px;}
 .state {font-size:12px;color:var(--muted);} .attention {color:var(--warning);}
 .empty {padding:8px 4px;}
 @media(max-width:760px){.project-tasks{padding:20px 16px;}.state{max-width:110px;}}
</style>
