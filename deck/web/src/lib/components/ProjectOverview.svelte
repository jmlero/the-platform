<script lang="ts">
 import { ArrowUpRight, Plus } from '@lucide/svelte';
 import { taskLabel, workspaceQueues } from '$lib/project-work';
 import type { Task } from '$lib/types';

 export let tasks:Task[]=[];
 export let onselect:(task:Task)=>Promise<void>;
 export let oncreate:()=>void;

 $: queues=workspaceQueues(tasks);
</script>

<section class="overview" aria-label="Project work">
 <header><div><small>Project work</small><h2>What’s happening</h2></div><button type="button" on:click={oncreate}><Plus size={15}/>Add task</button></header>
 <section class="group" aria-label="In progress">
  <h3>In progress <span>{queues.inProgress.length}</span></h3>
  {#each queues.inProgress as task (task.id)}
   <button class="task" type="button" on:click={()=>onselect(task)}><span><strong>{task.title}</strong><small>{taskLabel(task)}</small></span><ArrowUpRight size={14}/></button>
  {:else}<p class="empty">No work in progress.</p>{/each}
 </section>
 <section class="group" aria-label="To do">
  <h3>To do <span>{queues.todo.length}</span></h3>
  {#each queues.todo as task (task.id)}
   <button class="task" type="button" on:click={()=>onselect(task)}><span><strong>{task.title}</strong><small>{taskLabel(task)}</small></span><ArrowUpRight size={14}/></button>
  {:else}<p class="empty">No tasks to do.</p>{/each}
 </section>
 <section class="group" aria-label="Recently completed">
  <h3>Recently completed</h3>
  {#each queues.recent as task (task.id)}
   <button class="task" type="button" on:click={()=>onselect(task)}><span><strong>{task.title}</strong><small>{taskLabel(task)}</small></span><ArrowUpRight size={14}/></button>
  {:else}<p class="empty">No completed tasks yet.</p>{/each}
 </section>
</section>

<style>
 .overview{width:clamp(300px,32%,420px);min-width:300px;min-height:0;overflow-y:auto;padding:28px 24px;border-right:1px solid var(--line);background:var(--panel);}
 header{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:30px;}
 header small{display:block;color:var(--subtle);font-size:11px;}
 h2{margin:4px 0 0;font-size:18px;font-weight:600;letter-spacing:-.35px;}
 header button{display:flex;align-items:center;gap:5px;flex-shrink:0;padding:8px 10px;border:1px solid var(--line);border-radius:6px;background:var(--panel);color:var(--text);font:inherit;font-size:12px;cursor:pointer;}
 .group{margin-bottom:28px;}
 h3{display:flex;align-items:center;gap:8px;margin:0;padding:0 0 10px;border-bottom:1px solid var(--line);font-size:13px;font-weight:600;}
 h3 span{color:var(--subtle);font-size:12px;font-weight:400;}
 .task{display:flex;align-items:center;justify-content:space-between;gap:10px;width:100%;padding:12px 2px;border:0;border-bottom:1px solid var(--line);background:transparent;color:var(--text);text-align:left;font:inherit;cursor:pointer;}
 .task:hover{background:var(--surface-hover);}
 .task>span{display:grid;min-width:0;gap:4px;}
 .task strong{font-size:13px;font-weight:500;overflow-wrap:anywhere;}
 .task small,.empty{color:var(--subtle);font-size:11px;}
 .empty{padding:10px 2px;margin:0;}
 @media(max-width:1050px){.overview{width:300px;padding:22px 16px;}}
 @media(max-width:760px){.overview{width:100%;min-width:0;min-height:auto;max-height:42dvh;border-right:0;border-bottom:1px solid var(--line);}.group{margin-bottom:18px;}}
</style>
