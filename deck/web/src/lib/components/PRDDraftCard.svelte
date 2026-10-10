<script lang="ts">
 import PRDProgressCard from './PRDProgressCard.svelte';
 import { onMount } from 'svelte';
 import { api } from '$lib/api';
 import type { ChannelMessage, Task, WorkloadProfile } from '$lib/types';
 export let message: ChannelMessage;
 export let onapproved: (task:Task)=>Promise<void>;
 let profile: WorkloadProfile|undefined;
 let minutes=30,tokens=20000,busy=false,error='',authorized=false;
 let requestID='';
 onMount(async()=>{
  try {profile=(await api.workloads(message.project_id)).profiles.find(p=>p.id==='repository/v2');if(profile){minutes=Math.min(minutes,profile.budget_minutes);tokens=Math.min(tokens,profile.max_tokens);}}
  catch(caught){error=caught instanceof Error?caught.message:'Could not load publication limits.';}
 });
 async function approve(){
  if(!profile||!message.prd||busy)return;
  busy=true;error='';requestID ||= crypto.randomUUID();
  try {
   const task=await api.approvePRD(message.id,{digest:message.prd.digest,profile_digest:profile.digest,budget:{time_minutes:minutes,max_tokens:tokens,max_attempts:profile.min_attempts,max_cost_minor:0,currency:'USD',max_concurrency:1,authority:profile.authority}},requestID);
   authorized=true;await onapproved(task);
  } catch(caught){error=caught instanceof Error?caught.message:'Could not approve PRD.';}
  finally{busy=false;}
 }
</script>

{#if message.prd}
 <section aria-label={`PRD draft: ${message.prd.title}`} class="prd-draft">
  <strong>{message.prd.title}</strong>
  <span class="path">{message.prd.path}</span>
  <small>{message.prd.repository.repository} · {message.prd.repository.base_branch}</small>
  <pre>{message.prd.content}</pre>
  {#if authorized||message.promoted_task_id}<p class="authorized">Publication authorized</p>
  {:else if profile}
   <form on:submit|preventDefault={approve}>
    <p>Approve this exact text for publication. Marina will review it, run the project checks, and land it on the repository’s base branch. This does not authorize implementing the PRD.</p>
    <div class="limits">
     <label>Time limit (minutes)<input type="number" min="1" max={profile.budget_minutes} bind:value={minutes} required disabled={busy}/></label>
     <label>Token limit<input type="number" min="1" max={profile.max_tokens} bind:value={tokens} required disabled={busy}/></label>
    </div>
    <small>One task · {profile.min_attempts} runtime attempts · bounded by these time and token limits</small>
    <button type="submit" disabled={busy}>{busy?'Saving approval…':'Approve PRD and publish'}</button>
   </form>
  {:else}<p>Publication requires an available repository landing profile.</p>{/if}
  {#if message.promoted_task_id}<PRDProgressCard {message} onopen={onapproved}/>{/if}
  {#if error}<p role="alert">{error}</p>{/if}
 </section>
{/if}

<style>
 .prd-draft{display:grid;gap:8px;margin:14px 0;padding:16px;border:1px solid var(--line);border-radius:10px;background:var(--panel-soft);font-size:13px;}
 .path{overflow-wrap:anywhere;color:var(--accent);}small{color:var(--muted);line-height:1.5;}pre{white-space:pre-wrap;overflow-wrap:anywhere;font:inherit;line-height:1.7;padding:12px;background:var(--panel);border:1px solid var(--line);border-radius:6px;max-height:480px;overflow:auto;margin:4px 0;}form{display:grid;gap:12px;}p{margin:0;line-height:1.6;}.limits{display:flex;flex-wrap:wrap;gap:12px;}label{display:grid;gap:6px;flex:1;min-width:120px;}input{box-sizing:border-box;width:100%;min-height:36px;padding:8px;border:1px solid var(--line-strong);border-radius:6px;background:var(--panel);color:var(--text);font:inherit;}button{min-height:40px;padding:10px;border:0;border-radius:6px;background:var(--accent);color:white;font:inherit;cursor:pointer;}button:disabled{opacity:.6;cursor:wait;}[role=alert]{color:var(--danger);}.authorized{color:var(--success);}
</style>
