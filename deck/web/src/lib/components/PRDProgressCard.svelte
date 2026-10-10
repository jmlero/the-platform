<script lang="ts">
 import { api } from '$lib/api';
 import type { ChannelMessage, PRDProgress, Task } from '$lib/types';
 export let message:ChannelMessage;
 export let onopen:(task:Task)=>Promise<void>;
 let progress:PRDProgress|undefined,busy=false,error='',acceptRequest:{digest:string;id:string}|undefined,request:{body:string;id:string}|undefined;
 let feedback='';
 let loadSequence=0;
 $: if(message.promoted_task_id)void refresh(message.promoted_task_id);
 async function refresh(id:string){const sequence=++loadSequence;try{const result=await api.prdProgress(id);if(sequence===loadSequence)progress=result;}catch(caught){if(sequence===loadSequence)error=String(caught);}}
 async function accept(){if(!progress||busy)return;busy=true;error='';if(acceptRequest?.digest!==progress.results_digest)acceptRequest={digest:progress.results_digest,id:crypto.randomUUID()};try{progress=await api.acceptPRD(progress.publication.id,progress.results_digest,acceptRequest.id);}catch(caught){error=caught instanceof Error?caught.message:'Could not accept PRD.';}finally{busy=false;}}
 async function propose(){if(!progress||busy)return;const text=feedback.trim();const body=`Propose ordered ${text?'follow-up ':''}tasks for the published PRD ${progress.publication.prd!.path}. publication_task_id=${progress.publication.id} revision=${progress.publication.landing!.commit}.${text?' Requested changes: '+text:''} Present a proposal for explicit approval.`;if(request?.body!==body)request={body,id:crypto.randomUUID()};busy=true;error='';try{await api.invokeConversation(message.channel_id,{agent_id:message.author_id,thread_id:message.thread_id,body,request_id:request.id});request=undefined;feedback='';}catch(caught){error=caught instanceof Error?caught.message:'Could not request task proposal.';}finally{busy=false;}}
</script>
{#if progress}
 <section aria-label="PRD progress" class="progress">
  {#if progress.publication.landing}
   <p class="success">PRD published · {progress.publication.landing.commit.slice(0,12)}</p>
   {#if progress.tasks.length}
    <strong>{progress.tasks.filter(task=>task.landing).length} of {progress.tasks.length} tasks landed</strong>
    <ol>{#each progress.tasks as task}<li><button class="task" on:click={()=>onopen(task)}>{task.title}</button><span>{task.landing?'Landed · '+task.landing.commit.slice(0,12):task.display_state||task.status}</span></li>{/each}</ol>
   {:else}<p>No implementation tasks approved yet.</p>{/if}
   {#if progress.acceptance?.results_digest===progress.results_digest}<p class="success">PRD accepted</p>
   {:else if progress.ready_to_accept}<button disabled={busy} on:click={accept}>Accept PRD</button>
   {:else if progress.tasks.length}<p>Acceptance becomes available when all approved tasks land with current evidence.</p>{/if}
   <form on:submit|preventDefault={propose}>
    {#if progress.plans.length}<label>Requested PRD follow-ups<textarea bind:value={feedback} maxlength="2000" required disabled={busy}></textarea></label>{/if}
    <button disabled={busy}>{progress.plans.length?'Request follow-up proposal':'Propose implementation tasks'}</button>
   </form>
  {:else}<p>Publication: {progress.publication.display_state||progress.publication.status}</p>{/if}
  {#if error}<p role="alert">{error}</p>{/if}
 </section>
{/if}
<style>
 .progress{display:grid;gap:12px;border-top:1px solid var(--line);padding-top:12px;}p{margin:0;line-height:1.6;}ol{margin:0;padding-left:24px;}li{padding:6px 0;}li span{display:block;color:var(--muted);font-size:12px;}button{min-height:40px;padding:10px;border:0;border-radius:6px;background:var(--accent);color:white;cursor:pointer;}button:disabled{opacity:.6;cursor:wait;}button.task{padding:0;background:transparent;color:var(--accent);text-align:left;}form,label{display:grid;gap:8px;}textarea{min-height:72px;padding:8px;border:1px solid var(--line-strong);border-radius:6px;background:var(--panel);color:var(--text);font:inherit;resize:vertical;}.success{color:var(--success);}[role=alert]{color:var(--danger);}
</style>
