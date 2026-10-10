<script lang="ts">
 import { onMount } from 'svelte';
 import { api } from '$lib/api';
 import type { ChannelMessage, Task, WorkloadProfile } from '$lib/types';
 export let message:ChannelMessage;
 export let onapproved:(task:Task)=>Promise<void>;
 let profile:WorkloadProfile|undefined,minutes=30,tokens=20000,busy=false,error='',requestID='';
 onMount(async()=>{try{profile=(await api.workloads(message.project_id)).profiles.find(p=>p.id==='repository/v2');if(profile){minutes=Math.min(minutes,profile.budget_minutes);tokens=Math.min(tokens,profile.max_tokens);}}catch(caught){error=String(caught);}});
 async function approve(){if(!profile||!message.prd_tasks||busy)return;busy=true;error='';requestID ||= crypto.randomUUID();try{const tasks=await api.approvePRDTasks(message.id,{digest:message.prd_tasks.digest,profile_digest:profile.digest,budget:{time_minutes:minutes,max_tokens:tokens,max_attempts:profile.min_attempts,max_cost_minor:0,currency:'USD',max_concurrency:1,authority:profile.authority}},requestID);await onapproved(tasks[0]);}catch(caught){error=caught instanceof Error?caught.message:'Could not approve tasks.';}finally{busy=false;}}
</script>
{#if message.prd_tasks}
 <section aria-label="PRD task proposal" class="card">
  <strong>Tasks for {message.prd_tasks.source.path}</strong>
  <small>PRD revision {message.prd_tasks.revision}</small>
  <ol>{#each message.prd_tasks.tasks as task}<li><strong>{task.title}</strong><p>{task.description}</p><p>Acceptance: {task.acceptance_criteria}</p></li>{/each}</ol>
  {#if message.prd_tasks.approval_digest}<p class="success">Ordered tasks authorized</p>
  {:else if profile}
   <form on:submit|preventDefault={approve}>
    <p>Approve all {message.prd_tasks.tasks.length} tasks in this order. Each task waits for its predecessor to land.</p>
    <label>Time limit per task (minutes)<input type="number" min="1" max={profile.budget_minutes} bind:value={minutes} required disabled={busy}/></label>
    <label>Token limit per task<input type="number" min="1" max={profile.max_tokens} bind:value={tokens} required disabled={busy}/></label>
    <small>Total limits: {minutes*message.prd_tasks.tasks.length} minutes and {tokens*message.prd_tasks.tasks.length} tokens · {profile.min_attempts} runtime attempts per task</small>
    <button disabled={busy}>{busy?'Saving approval…':'Approve ordered tasks'}</button>
   </form>
  {:else}<p>Task approval requires an available repository landing profile.</p>{/if}
  {#if error}<p role="alert">{error}</p>{/if}
 </section>
{/if}
<style>
 .card{display:grid;gap:12px;margin:14px 0;padding:16px;border:1px solid var(--line);border-radius:10px;background:var(--panel-soft);font-size:13px;}small{color:var(--muted);overflow-wrap:anywhere;}ol{padding-left:24px;margin:0;}li{padding:8px 0;}p{margin:6px 0;line-height:1.6;white-space:pre-wrap;}form,label{display:grid;gap:8px;}input{padding:8px;min-height:36px;border:1px solid var(--line-strong);border-radius:6px;background:var(--panel);color:var(--text);}button{min-height:40px;padding:10px;border:0;border-radius:6px;background:var(--accent);color:white;cursor:pointer;}button:disabled{opacity:.6;cursor:wait;}.success{color:var(--success);}[role=alert]{color:var(--danger);}
</style>
