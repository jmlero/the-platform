<script lang="ts">
 import { ArrowUp, MessageSquare, Plus, X, ArrowUpRight, FileText, Check, CornerDownRight } from '@lucide/svelte';
 import { onDestroy, onMount, tick } from 'svelte';
 import { api } from '$lib/api';
 import { configuredConversationAgents, mentionedConversationAgent } from '$lib/conversation-agents';
 import { createChannelHistory } from '$lib/channel-history';
 import { taskLabel } from '$lib/project-work';
 import CreateTaskModal from './CreateTaskModal.svelte';
 import { formatDate } from '$lib/format';
 import type { Agent, Channel, ChannelMessage, ConversationQuestion, ConversationThread, CreateTaskInput, Task } from '$lib/types';
 export let channels:Channel[]=[];
 export let tasks:Task[]=[];
 export let agents:Agent[]=[];
 export let onpromoted:(task:Task)=>Promise<void>;
 export let onopen:(task:Task)=>Promise<void>;
 export let refreshVersion=0;
 let selectedID=channels[0]?.id??'', messages:ChannelMessage[]=[], body='',busy=false,error='',loading=false;
 let promoting:ChannelMessage|null=null,selectedMessages:string[]=[],threadID='',noteSaved='';
 let activeThread:ConversationThread|null=null,selectedAgentID='',invocationNotice='';
 let questions:ConversationQuestion[]=[],answerText='',answerQuestionID='',answerRequest:{signature:string;id:string}|null=null;
 let pendingRequest:{signature:string;id:string}|null=null;
 let scroll:HTMLDivElement;
 const history=createChannelHistory(items=>messages=items,message=>error=message,state=>loading=state);
 $: if(selectedID){refreshVersion;void history.refresh(selectedID);}
 $: if(threadID){refreshVersion;void refreshThread(threadID);}
 $: roots=messages.filter(m=>!m.thread_id||messages.find(n=>n.thread_id===m.thread_id)?.id===m.id);
 $: shown=threadID?messages.filter(m=>m.thread_id===threadID):roots;
 $: projectID=channels.find(channel=>channel.id===selectedID)?.project_id??'';
 $: conversationAgents=configuredConversationAgents(agents,projectID);
 $: updates=tasks.filter(t=>['done','blocked','failed','waiting_decision','running','verifying'].includes(t.status));
 onDestroy(()=>history.close());
 onMount(()=>{const id=new URL(window.location.href).searchParams.get('thread');if(id)void api.getThread(id).then(thread=>{if(channels.some(channel=>channel.id===thread.channel_id)){selectedID=thread.channel_id;setThread(thread,false);}}).catch(()=>{});});
 async function refreshThread(id:string){try{const [thread,page]=await Promise.all([api.getThread(id),api.conversationQuestions(id)]);if(threadID===id){activeThread=thread;questions=page.items;selectedAgentID=thread.selected_agent_id??selectedAgentID;}}catch{/* Channel history shows the recoverable error. */}}
 function setThread(thread:ConversationThread|null,updateURL=true){activeThread=thread;threadID=thread?.id??'';selectedAgentID=thread?.selected_agent_id??'';selectedMessages=[];questions=[];answerText='';answerQuestionID='';invocationNotice='';if(updateURL){const url=new URL(window.location.href);if(thread)url.searchParams.set('thread',thread.id);else url.searchParams.delete('thread');window.history.replaceState(window.history.state,'',url);}}
 function requestID(signature:string){if(pendingRequest?.signature!==signature)pendingRequest={signature,id:crypto.randomUUID()};return pendingRequest.id;}
 async function post(){if(!body.trim()||busy)return;busy=true;error='';invocationNotice='';const channel=selectedID,currentThread=threadID,text=body.trim();const agent=activeThread?.selected_agent_id||selectedAgentID||mentionedConversationAgent(text,agents,projectID)?.id||'';try{if(agent){const result=await api.invokeConversation(channel,{agent_id:agent,thread_id:currentThread||undefined,body:text,request_id:requestID(`${channel}:${currentThread}:${agent}:${text}`)});pendingRequest=null;setThread(result.thread);invocationNotice='Request recorded. The configured agent will reply here when available.';}else{await api.postChannelMessage(channel,text,{},currentThread);}if(selectedID===channel){body='';await history.refresh(channel);await tick();scroll?.scrollTo({top:scroll.scrollHeight,behavior:'smooth'});}}catch(caught){error=caught instanceof Error?caught.message:'Could not save message.';}finally{busy=false;}}
 async function openThread(message:ChannelMessage){try{const thread=await api.createThread(selectedID,message.id);setThread(thread);await history.refresh(selectedID);}catch(caught){error=String(caught);}}
 async function invokeExisting(message:ChannelMessage){const agent=activeThread?.selected_agent_id||selectedAgentID;if(!agent){error='Select a conversation agent first.';return;}busy=true;error='';try{const result=await api.invokeConversation(selectedID,{agent_id:agent,message_id:message.id,thread_id:threadID,request_id:requestID(`${selectedID}:${threadID}:${agent}:${message.id}`)});pendingRequest=null;setThread(result.thread);invocationNotice='Request recorded. The configured agent will reply here when available.';await history.refresh(selectedID);}catch(caught){error=caught instanceof Error?caught.message:'Could not request agent.';}finally{busy=false;}}
 async function answerQuestion(question:ConversationQuestion){const answer=answerText.trim();if(!answer||busy)return;const signature=`${question.id}:${answer}`;if(answerRequest?.signature!==signature)answerRequest={signature,id:crypto.randomUUID()};busy=true;error='';try{const updated=await api.answerConversationQuestion(question.id,answer,answerRequest.id);questions=questions.map(item=>item.id===updated.id?updated:item);answerText='';answerQuestionID='';answerRequest=null;await history.refresh(selectedID);invocationNotice='Answer saved. The agent can continue if this turn is still active.';}catch(caught){error=caught instanceof Error?caught.message:'Could not save answer.';}finally{busy=false;}}
 async function promote(input:CreateTaskInput){if(!promoting)return;const task=await api.promoteMessage(promoting.id,{...input,status:'backlog',context_message_ids:[...new Set([...selectedMessages,promoting.id])]});promoting=null;selectedMessages=[];await history.refresh(selectedID);if(threadID)activeThread=await api.getThread(threadID);await onpromoted(task);}
 async function saveNote(message:ChannelMessage){busy=true;error='';try{await api.putContextSource({id:`note-${message.id}`,expected_revision:0,kind:'document',title:message.body.slice(0,70),content:`${message.body}\n\nSource: channel message ${message.id}`,trust:'data',state:'active'},message.project_id);noteSaved=message.id;}catch(caught){error=caught instanceof Error?caught.message:'Could not save note.';}finally{busy=false;}}
 function keydown(event:KeyboardEvent){if(event.key==='Enter'&&!event.shiftKey&&!event.isComposing){event.preventDefault();void post();}}
</script>

<section class="conversation" aria-label="Project conversation">
 {#if channels.length>1}<div class="earlier"><label>Conversation<select bind:value={selectedID} on:change={()=>{setThread(null);body='';}}>{#each channels as channel}<option value={channel.id}>{channel.name}</option>{/each}</select></label><span>Existing project history</span></div>{/if}
 {#if threadID}<div class="thread-heading"><CornerDownRight size={16}/><strong>Thread</strong>{#if activeThread?.selected_agent_id}<span>Addressed to {agents.find(agent=>agent.id===activeThread?.selected_agent_id)?.name??activeThread.selected_agent_id}</span>{/if}<button type="button" aria-label="Back to channel" on:click={()=>{setThread(null);body='';}}><X size={16}/></button></div>{/if}
 {#if error}<p class="channel-error" role="alert">{error}<button on:click={()=>history.refresh(selectedID)}>Reload</button></p>{/if}
 {#if invocationNotice}<p class="invocation-notice" role="status">{invocationNotice}</p>{/if}
 <div class="messages" bind:this={scroll} aria-busy={loading}>
  <div class="message-column">
   {#if !messages.length&&!loading}<div class="conversation-start"><div class="start-icon"><MessageSquare size={23}/></div><h2>Start with a conversation</h2><p>Explore an idea, capture what matters, and shape the next piece of work.</p></div>{/if}
   {#each shown as message (message.id)}
    {@const linkedTasks=[...new Set([...(message.promoted_task_id?[message.promoted_task_id]:[]),...(message.references?.task_ids??[])])].map(id=>tasks.find(t=>t.id===id)).filter((task):task is Task=>!!task)}
    {@const replies=message.thread_id?messages.filter(m=>m.thread_id===message.thread_id&&m.id!==message.id).length:0}
    {@const question=questions.find(item=>item.message_id===message.id)}
    <article class="message">
     <div class:agent-avatar={message.author_type==='agent'} class="avatar">{message.author_type==='human'?'Y':message.author_type==='agent'?'A':'D'}</div>
     <div class="message-content"><header><strong>{message.author_type==='human'?'You':message.author_type==='agent'?message.author_id:'Deck'}</strong><time datetime={new Date(message.created_at).toISOString()}>{formatDate(message.created_at)}</time></header><p>{message.body}</p>
      {#if message.invoked_agent_id}<small class="invoked">Agent requested: {agents.find(agent=>agent.id===message.invoked_agent_id)?.name??message.invoked_agent_id}</small>{/if}
      {#if question?.status==='pending'&&threadID}
       <div class="question-answer"><strong>Answer this question</strong>
        {#if question.choices.length}<div class="question-choices">{#each question.choices as choice}<button type="button" disabled={busy} on:click={()=>{answerQuestionID=question.id;answerText=choice;}}>{choice}</button>{/each}</div>{/if}
        <div class="question-entry"><input aria-label={`Answer ${question.text}`} value={answerQuestionID===question.id?answerText:''} on:input={event=>{answerQuestionID=question.id;answerText=event.currentTarget.value;}} placeholder="Your answer"/><button type="button" disabled={busy||answerQuestionID!==question.id||!answerText.trim()} on:click={()=>answerQuestion(question)}>Send answer</button></div>
       </div>
      {/if}
      {#each linkedTasks as linked}<button class="linked-task" type="button" on:click={()=>onopen(linked)}><Check size={14}/><span>{linked.title}</span><small>{taskLabel(linked)}</small><ArrowUpRight size={13}/></button>{/each}
      {#if message.references?.evidence_ids?.length}<small class="invoked">{message.references.evidence_ids.length} evidence record{message.references.evidence_ids.length===1?'':'s'} in task details</small>{/if}
      <footer>
       {#if !threadID}<button type="button" on:click={()=>openThread(message)}><MessageSquare size={13}/>{replies?`${replies} replies`:'Reply in thread'}</button>{/if}
       {#if threadID&&message.author_type==='human'&&!message.invoked_agent_id&&conversationAgents.length}<button type="button" disabled={busy} on:click={()=>invokeExisting(message)}><ArrowUpRight size={13}/>Ask agent</button>{/if}
       {#if message.author_type==='human'&&!message.promoted_task_id}<button type="button" on:click={()=>promoting=message}><Plus size={13}/>Create task</button>{/if}
       {#if message.author_type!=='system'}<button type="button" disabled={busy||noteSaved===message.id} on:click={()=>saveNote(message)}><FileText size={13}/>{noteSaved===message.id?'Note saved':'Save note'}</button>{/if}
       <label><input type="checkbox" bind:group={selectedMessages} value={message.id} disabled={!selectedMessages.includes(message.id)&&selectedMessages.length>=19}/>Context</label>
      </footer>
     </div>
    </article>
   {/each}
   {#if threadID&&activeThread?.task_ids.length}<div class="thread-tasks"><strong>Tasks from this thread</strong>{#each activeThread.task_ids as taskID}{@const task=tasks.find(item=>item.id===taskID)}{#if task}<button type="button" on:click={()=>onopen(task)}>{task.title}<small>{taskLabel(task)}</small><ArrowUpRight size={13}/></button>{:else}<span>{taskID}</span>{/if}{/each}</div>{/if}
   {#if !threadID&&updates.length}<details class="work-updates"><summary>Work updates <span>{updates.length}</span></summary>{#each updates as task}<button type="button" on:click={()=>onopen(task)}><span>{task.title}</span><small>{taskLabel(task)}</small><ArrowUpRight size={13}/></button>{/each}</details>{/if}
  </div>
 </div>
 <div class="composer-area"><div class="composer-column">
  {#if selectedMessages.length}<div class="context-hint">{selectedMessages.length} message{selectedMessages.length===1?'':'s'} selected for the next task<button type="button" on:click={()=>selectedMessages=[]}>Clear</button></div>{/if}
  <form class="composer" on:submit|preventDefault={post}>
   <textarea aria-label={threadID?'Reply in thread':'Message project channel'} bind:value={body} rows="3" on:keydown={keydown} placeholder={threadID?'Reply to this discussion…':'Write a message, capture an idea…'}></textarea>
   <div class="composer-tools"><label>Agent <select aria-label="Conversation agent" bind:value={selectedAgentID} disabled={!!activeThread?.selected_agent_id||busy} on:change={()=>pendingRequest=null}><option value="">Save only</option>{#each conversationAgents as agent}<option value={agent.id}>{agent.name} ({agent.status})</option>{/each}</select></label><button type="submit" aria-label="Send message" disabled={busy||!body.trim()}><ArrowUp size={18}/></button></div>
  </form>
  <p class="composer-note">Enter to send · Shift + Enter for a new line. {#if !conversationAgents.length}No conversation agent is configured. Messages are saved.{:else}Selecting an agent records a request in this thread. Replies and linked work appear here when the agent runs.{/if}</p>
 </div></div>
</section>
{#if promoting}<CreateTaskModal initialTitle={promoting.body.length>80?`${promoting.body.slice(0,77)}…`:promoting.body} initialDescription={promoting.body} backlogOnly={true} onclose={()=>promoting=null} oncreate={promote}/>{/if}

<style>
 .conversation{display:flex;flex-direction:column;min-height:0;flex:1;}.messages{flex:1;min-height:0;overflow-y:auto;padding:32px 36px 20px;}.message-column,.composer-column{width:100%;max-width:860px;margin:0 auto;}.message{display:flex;gap:13px;margin-bottom:28px;}.avatar{display:grid;place-items:center;flex-shrink:0;width:32px;height:32px;border-radius:7px;background:#e9edf4;color:#4a5974;font-size:13px;font-weight:600;}.agent-avatar{background:var(--accent-soft);color:var(--accent);}.message-content{min-width:0;flex:1;}header{display:flex;align-items:baseline;gap:10px;padding:3px 0;}header strong{font-size:13px;font-weight:600;}time{font-size:11px;color:var(--subtle);}.message-content>p{font-size:14px;line-height:1.8;margin:5px 0 6px;white-space:pre-wrap;overflow-wrap:anywhere;color:var(--text);}
 footer{display:flex;flex-wrap:wrap;align-items:center;gap:13px;margin-top:8px;}footer button,footer label{display:flex;align-items:center;gap:5px;font-size:11px;color:var(--subtle);}button{cursor:pointer;font:inherit;}footer button{padding:5px 0;background:transparent;border:0;}footer button:hover{color:var(--accent);}footer label{margin-left:auto;cursor:pointer;}footer input{width:13px;height:13px;}button:disabled{opacity:.55;cursor:default;}
 .composer-area{padding:12px 36px 18px;background:var(--panel);}.composer{border:1px solid var(--line-strong);border-radius:10px;overflow:hidden;background:var(--panel);}.composer:focus-within{border-color:var(--accent);}.composer textarea{display:block;width:100%;min-height:88px;max-height:240px;resize:vertical;border:0;padding:15px 17px;background:transparent;color:var(--text);font:inherit;font-size:14px;line-height:1.6;}.composer textarea:focus{outline:none;}.composer-tools{display:flex;align-items:center;justify-content:space-between;padding:8px 10px 10px 17px;color:var(--subtle);font-size:11px;}.composer-tools button{display:grid;place-items:center;width:32px;height:32px;border:0;border-radius:6px;background:var(--accent);color:white;}.composer-note{font-size:11px;color:var(--subtle);margin:9px 0 0;line-height:1.5;}.conversation-start{margin:70px auto 80px;max-width:390px;text-align:center;}.start-icon{display:grid;place-items:center;margin:auto;width:50px;height:50px;border:1px solid var(--line);border-radius:12px;color:var(--accent);}.conversation-start h2{font-size:23px;letter-spacing:-.6px;font-weight:500;margin:20px 0 10px;}.conversation-start p{font-size:14px;line-height:1.8;color:var(--subtle);}
 .linked-task{display:flex;gap:10px;align-items:center;width:100%;padding:12px;margin:12px 0 0;border:1px solid var(--line);border-radius:6px;background:var(--panel-soft);color:var(--text);text-align:left;font-size:13px;}.linked-task span{flex:1;}.linked-task small{color:var(--muted);font-size:11px;}.work-updates{margin:30px 0;color:var(--muted);font-size:12px;}.work-updates summary{cursor:pointer;padding:12px 0;border-top:1px solid var(--line);}.work-updates summary span{margin-left:8px;color:var(--subtle);}.work-updates button{display:flex;align-items:center;gap:10px;width:100%;padding:12px 0;border:0;background:transparent;text-align:left;font-size:13px;color:var(--text);}.work-updates button span{flex:1;}.work-updates small{color:var(--subtle);font-size:11px;}
 .thread-heading,.earlier{display:flex;align-items:center;gap:10px;padding:12px 32px;border-bottom:1px solid var(--line);font-size:12px;color:var(--muted);}.thread-heading strong{font-weight:500;}.thread-heading button{margin-left:auto;border:0;background:transparent;color:var(--subtle);}.earlier label{display:flex;gap:10px;align-items:center;}.earlier select{padding:5px;background:var(--panel);border:1px solid var(--line);border-radius:4px;color:var(--text);font:inherit;}.earlier>span{margin-left:auto;color:var(--subtle);}.context-hint{display:flex;justify-content:space-between;padding:8px 0;color:var(--accent);font-size:12px;}.context-hint button,.channel-error button{border:0;background:transparent;color:inherit;}.channel-error{display:flex;justify-content:space-between;margin:12px 32px;padding:12px;color:var(--danger);background:var(--danger-soft);border-radius:6px;font-size:13px;}
 .composer-tools label{display:flex;align-items:center;gap:8px;}.composer-tools select{max-width:180px;padding:5px;border:1px solid var(--line);border-radius:5px;background:var(--panel);color:var(--text);font:inherit;font-size:12px;}.invoked{display:block;margin:4px 0 8px;color:var(--accent);font-size:11px;}.invocation-notice{margin:10px 32px 0;padding:10px 12px;border-radius:6px;background:var(--accent-soft);color:var(--accent);font-size:12px;}.thread-tasks{display:grid;gap:6px;padding:16px 0;border-top:1px solid var(--line);}.thread-tasks strong{font-size:12px;color:var(--muted);}.thread-tasks button{display:flex;align-items:center;gap:8px;padding:8px;border:1px solid var(--line);border-radius:5px;background:var(--panel);color:var(--text);text-align:left;}.thread-tasks button small{margin-left:auto;color:var(--subtle);}
 .question-answer{display:grid;gap:9px;margin:12px 0;padding:12px;border:1px solid var(--line);border-radius:7px;background:var(--panel-soft);}.question-answer strong{font-size:12px;}.question-choices{display:flex;gap:7px;flex-wrap:wrap;}.question-choices button,.question-entry button{border:1px solid var(--line-strong);border-radius:5px;padding:6px 9px;background:var(--panel);color:var(--text);font-size:12px;}.question-entry{display:flex;gap:8px;}.question-entry input{flex:1;min-width:0;padding:7px 9px;border:1px solid var(--line-strong);border-radius:5px;background:var(--panel);color:var(--text);font:inherit;font-size:12px;}
 @media(max-width:760px){.messages{padding:24px 16px;}.composer-area{padding:8px 16px 14px;}.earlier{padding:12px 16px;}.earlier>span{display:none;}footer{gap:10px;}footer label{margin-left:0;}}
</style>
