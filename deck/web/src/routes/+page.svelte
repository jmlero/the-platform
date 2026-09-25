<script lang="ts">
 import { onMount } from 'svelte';
 import { Hash, Plus, Search, X, Menu, Inbox, Archive, ArrowUpRight, ChevronDown } from '@lucide/svelte';
 import BrandMark from '$lib/components/BrandMark.svelte';
 import ChannelsView from '$lib/components/ChannelsView.svelte';
 import ProjectTasks from '$lib/components/ProjectTasks.svelte';
 import ProjectContext from '$lib/components/ProjectContext.svelte';
 import DecisionInbox from '$lib/components/DecisionInbox.svelte';
 import TaskDrawer from '$lib/components/TaskDrawer.svelte';
 import CreateTaskModal from '$lib/components/CreateTaskModal.svelte';
 import { modal } from '$lib/dialog';
 import { api, subscribeToEvents, type OperatorSession } from '$lib/api';
 import { projectTasks, taskLabel } from '$lib/project-work';
 import type { Task, TaskDetail, Agent, Decision, CreateTaskInput, UpdateTaskInput, TaskStatus, AppEvent, ProjectChannel } from '$lib/types';

 let session:OperatorSession|null=null;
 let sessionChecking=true, authenticating=false, loading=true, connected=false, sidebarOpen=false;
 let loginToken='',loginError='',error='',search='';
 let projects:ProjectChannel[]=[], allTasks:Task[]=[], agents:Agent[]=[], decisions:Decision[]=[];
 let selectedProjectID='', activeView:'project'|'inbox'|'archive'='project';
 let tab:'conversation'|'tasks'|'context'='conversation';
 let selectedDetail:TaskDetail|null=null;
 let createOpen=false,projectOpen=false,projectBusy=false,projectError='',repository='',baseBranch='main';
 let channelRefreshVersion=0;
 let unsubscribe:(()=>void)|undefined;
 let timer:ReturnType<typeof setTimeout>|undefined;
 $: project=projects.find(p=>p.project.id===selectedProjectID)??projects[0];
 $: tasks=projectTasks(allTasks,project?.project.id??'');
 $: projectDecisions=decisions.filter(d=>tasks.some(t=>t.id===d.task_id));
 $: attention=tasks.filter(t=>['blocked','failed','waiting_decision'].includes(t.status)||(t.status==='done'&&!t.review));
 $: archive=allTasks.filter(t=>t.archived&&t.project_id===project?.project.id&&t.title.toLowerCase().includes(search.toLowerCase()));
 $: repo=project?.repository.binding?.repository||project?.repository.repository;

 onMount(()=>{
  selectedProjectID=localStorage.getItem('deck.project')??'';
  const reset=()=>{session=null;allTasks=[];projects=[];agents=[];decisions=[];selectedDetail=null;unsubscribe?.();connected=false;};
  window.addEventListener('deck:unauthenticated',reset);
  void initialize();
  return ()=>{window.removeEventListener('deck:unauthenticated',reset);unsubscribe?.();if(timer)clearTimeout(timer);};
 });
 async function initialize(){
  try{session=await api.session();startEvents();await load();}
  catch(caught){if(session)error=String(caught);}
  finally{sessionChecking=false;loading=false;}
 }
 async function login(){authenticating=true;loginError='';try{session=await api.login(loginToken);loginToken='';startEvents();await load();}catch(caught){loginError=caught instanceof Error?caught.message:'Could not sign in.';}finally{authenticating=false;}}
 function startEvents(){unsubscribe?.();unsubscribe=subscribeToEvents(handleEvent,state=>{connected=state;if(state){channelRefreshVersion++;void load();}});}
 async function load(){
  try{const [snapshot,workspaces]=await Promise.all([api.bootstrap(),api.projects()]);allTasks=snapshot.tasks;agents=snapshot.agents;decisions=snapshot.decisions;projects=workspaces;error='';}
  catch(caught){error=caught instanceof Error?caught.message:'Could not load the workspace.';}
  finally{loading=false;}
 }
 function handleEvent(event:AppEvent){
  if(event.type.startsWith('channel.')||event.type.startsWith('conversation.')||event.type.startsWith('run.')||event.type.startsWith('command.')||event.type.startsWith('decision.')||event.type==='plan.changes_requested'||event.type==='task.promoted_from_channel')channelRefreshVersion++;
  if(event.type==='task.deleted'&&selectedDetail?.task.id===event.payload?.task_id)selectedDetail=null;
  if(timer)clearTimeout(timer);
  timer=setTimeout(async()=>{await load();if(selectedDetail&&event.task_id===selectedDetail.task.id)await refreshSelected();},150);
 }
 function selectProject(id:string){selectedProjectID=id;activeView='project';tab='conversation';search='';sidebarOpen=false;localStorage.setItem('deck.project',id);}
 async function openTask(task:Task){try{selectedDetail=await api.task(task.id);}catch(caught){error=caught instanceof Error?caught.message:'Could not open task.';}}
 async function refreshSelected(){if(selectedDetail)selectedDetail=await api.task(selectedDetail.task.id);await load();}
 async function createTask(input:CreateTaskInput){const task=await api.createTask({...input,project_id:project?.project.id});createOpen=false;await load();await openTask(task);}
 async function moveTask(task:Task,status:TaskStatus){await api.moveTask(task.id,status);await load();}
 async function saveTask(task:Task,input:UpdateTaskInput){await api.updateTask(task.id,input);await load();}
 async function answerDecision(decision:Decision,answer:string){await api.answerDecision(decision.id,answer);await load();}
 async function retainTask(task:Task,action:'delete'|'archive'|'restore'){if(action==='delete')await api.deleteDraft(task);else await api.archiveTask(task,action==='archive');selectedDetail=null;await load();}
 async function promoted(task:Task){await load();await openTask(task);}
 async function createProject(){projectBusy=true;projectError='';try{const item=await api.createProject(repository.trim().replace(/^https:\/\/github\.com\//,'').replace(/\.git$/,''),baseBranch.trim());await load();selectProject(item.project.id);projectOpen=false;repository='';}catch(caught){projectError=caught instanceof Error?caught.message:'Could not create project.';}finally{projectBusy=false;}}
 async function ensureChannel(){if(!project)return;try{await api.createChannel(repo?.split('/').pop()||project.project.slug,project.project.id);await load();}catch(caught){error=String(caught);}}
</script>

<svelte:head><title>{repo||project?.project.name||'Projects'} · Deck</title></svelte:head>

{#if sessionChecking}
 <div class="auth-shell" role="status">Opening Deck…</div>
{:else if !session}
 <div class="auth-shell"><section class="auth-card"><BrandMark/><h1>Welcome to Deck</h1><p>Sign in to your workspace.</p><form on:submit|preventDefault={login}><label>Operator token<input type="password" autocomplete="current-password" bind:value={loginToken} required/></label>{#if loginError}<p role="alert">{loginError}</p>{/if}<button class="primary" disabled={authenticating||loginToken.length<32}>{authenticating?'Signing in…':'Sign in'}</button></form></section></div>
{:else}
 <div class="app-shell">
  {#if sidebarOpen}<button class="scrim" on:click={()=>sidebarOpen=false} aria-label="Close navigation"></button>{/if}
  <aside class:mobile-open={sidebarOpen} class="sidebar" id="project-navigation">
   <div class="brand"><BrandMark/><strong>Deck</strong></div>
   <div class="nav-heading"><span>Projects</span><button type="button" aria-label="Add project" on:click={()=>projectOpen=true}><Plus size={16}/></button></div>
   <nav aria-label="Projects">
    {#each projects as item (item.project.id)}
     {@const name=item.repository.binding?.repository||item.repository.repository||item.project.name}
     <button type="button" class:active={project?.project.id===item.project.id&&activeView==='project'} aria-current={project?.project.id===item.project.id&&activeView==='project'?'page':undefined} on:click={()=>selectProject(item.project.id)} title={name}><Hash size={17}/><span>{name.split('/').pop()}</span></button>
    {:else}<p class="nav-empty">Connect a repository to begin.</p>{/each}
   </nav>
   <div class="sidebar-bottom"><button type="button" class:active={activeView==='inbox'} on:click={()=>{activeView='inbox';sidebarOpen=false;}}><Inbox size={16}/><span>Needs attention</span>{#if attention.length}<small>{attention.length}</small>{/if}</button><button type="button" class:active={activeView==='archive'} on:click={()=>{activeView='archive';sidebarOpen=false;}}><Archive size={16}/><span>Archive</span></button><div class="connection"><i class:connected></i>{connected?'Connected':'Reconnecting…'}</div></div>
  </aside>
  <main>
   <header class="project-header">
    <div class="project-title"><button class="menu-button" type="button" on:click={()=>sidebarOpen=true} aria-label="Open navigation" aria-controls="project-navigation"><Menu size={18}/></button><Hash size={21}/><h1>{repo?.split('/').pop()||project?.project.name||'Projects'}</h1>{#if repo}<a href={`https://github.com/${repo}`} target="_blank" rel="noreferrer">{repo}<ArrowUpRight size={12}/></a>{/if}</div>
    {#if activeView==='project'}<nav class="tabs" aria-label="Project views"><button class:active={tab==='conversation'} aria-current={tab==='conversation'?'page':undefined} on:click={()=>{tab='conversation';search='';}}>Conversation</button><button class:active={tab==='tasks'} aria-current={tab==='tasks'?'page':undefined} on:click={()=>tab='tasks'}>Tasks{#if tasks.length}<span>{tasks.length}</span>{/if}</button><button class:active={tab==='context'} aria-current={tab==='context'?'page':undefined} on:click={()=>tab='context'}>Context</button></nav>{/if}
   </header>
   {#if error}<div class="error" role="alert">{error}<button on:click={()=>load()}>Retry</button></div>{/if}
   <div class:conversation-space={activeView==='project'&&tab==='conversation'} class="workspace">
    {#if loading}<p class="empty" role="status">Loading project…</p>
    {:else if !project}<div class="welcome"><Hash size={32}/><h2>A home for your next project</h2><p>Connect a GitHub repository, discuss the work, and build context together.</p><button class="primary" on:click={()=>projectOpen=true}><Plus size={15}/>Add project</button></div>
    {:else if activeView==='archive'}<section class="secondary-view"><h2>Archive</h2><p>Retained tasks and their history.</p>{#each archive as task}<button class="result-row" on:click={()=>openTask(task)}><strong>{task.title}</strong><span>{taskLabel(task)}</span></button>{:else}<p class="empty">No archived tasks in this project.</p>{/each}</section>
    {:else if activeView==='inbox'}<section class="secondary-view"><h2>Needs attention</h2><DecisionInbox decisions={projectDecisions} {tasks} onanswer={answerDecision} onselect={openTask}/>{#each attention.filter(t=>t.status!=='waiting_decision') as task}<button class="result-row" on:click={()=>openTask(task)}><strong>{task.title}</strong><span>{taskLabel(task)}</span></button>{/each}{#if !attention.length}<p class="empty">Nothing needs your attention.</p>{/if}</section>
    {:else if tab==='conversation'}
     {#key project.project.id}
      {#if project.channels.length}<ChannelsView channels={project.channels} refreshVersion={channelRefreshVersion} onpromoted={promoted} onopen={openTask} {tasks} {agents}/>
      {:else}<div class="welcome"><Hash size={32}/><h2>Start the conversation</h2><p>Keep discussions, decisions, and ideas together for this project.</p><button class="primary" on:click={ensureChannel}>Open project channel</button></div>{/if}
     {/key}
    {:else if tab==='tasks'}<div class="task-search"><label><Search size={15}/><input bind:value={search} placeholder="Find a task…" aria-label="Search project tasks"/>{#if search}<button aria-label="Clear search" on:click={()=>search=''}><X size={14}/></button>{/if}</label></div><ProjectTasks {tasks} {search} onselect={openTask} oncreate={()=>createOpen=true}/>
    {:else}<div class="context-space">{#key project.project.id}<ProjectContext projectID={project.project.id}/>{/key}</div>{/if}
   </div>
  </main>
 </div>
 {#if selectedDetail}<TaskDrawer detail={selectedDetail} {agents} projectTasks={tasks} onclose={()=>selectedDetail=null} onrefresh={refreshSelected} onmove={moveTask} onsave={saveTask} onanswer={answerDecision} onretention={retainTask}/>{/if}
 {#if createOpen}<CreateTaskModal onclose={()=>createOpen=false} oncreate={createTask}/>{/if}
 {#if projectOpen}<dialog class="project-dialog" use:modal aria-labelledby="new-project-title" on:cancel|preventDefault={()=>projectOpen=false}><header><h2 id="new-project-title">Connect a project</h2><button aria-label="Close" on:click={()=>projectOpen=false}><X size={17}/></button></header><p>One GitHub repository. One project channel.</p><form on:submit|preventDefault={createProject}><label>GitHub repository<input data-dialog-focus bind:value={repository} placeholder="owner/repository" required/></label><label>Base branch<input bind:value={baseBranch} required/></label><p class="form-note">Repository access is validated by the configured execution service. New projects may need installation setup before agents can work on them.</p>{#if projectError}<p class="error" role="alert">{projectError}</p>{/if}<button class="primary" disabled={projectBusy}>{projectBusy?'Connecting…':'Create project'}</button></form></dialog>{/if}
{/if}

<style>
 .app-shell{display:flex;height:100dvh;overflow:hidden;background:var(--panel);}
 .sidebar{display:flex;flex-direction:column;width:224px;flex-shrink:0;padding:28px 12px 18px;background:#f5f6f8;border-right:1px solid var(--line);}
 .brand{display:flex;align-items:center;gap:10px;padding:0 12px 34px;color:var(--accent);}.brand strong{font-size:21px;letter-spacing:-.6px;color:var(--text);font-weight:650;}
 .nav-heading{display:flex;align-items:center;justify-content:space-between;padding:0 12px 8px;font-size:12px;color:var(--subtle);font-weight:500;}
 button{font:inherit;cursor:pointer;}button:disabled{opacity:.55;cursor:default;}
 .nav-heading button,.project-title button,.project-dialog header button{display:grid;place-items:center;width:32px;height:32px;border:0;border-radius:5px;background:transparent;color:var(--subtle);}
 .sidebar nav{display:grid;gap:3px;}.sidebar nav button,.sidebar-bottom>button{display:flex;align-items:center;gap:10px;width:100%;min-height:42px;padding:10px 12px;border:0;border-radius:6px;background:transparent;color:var(--muted);text-align:left;font-size:14px;}
 .sidebar nav button span,.sidebar-bottom>button span{flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;}.sidebar nav button:hover,.sidebar-bottom>button:hover{background:#e9edef;}.sidebar button.active{background:#e3eaee;color:#163c4c;font-weight:600;}.sidebar small{font-size:11px;color:var(--warning);}
 .sidebar-bottom{margin-top:auto;padding-top:24px;}.connection{display:flex;align-items:center;gap:7px;padding:22px 12px 0;font-size:11px;color:var(--subtle);}.connection i{width:6px;height:6px;background:var(--warning);border-radius:50%;}.connection i.connected{background:var(--success);}.nav-empty{font-size:13px;padding:0 12px;color:var(--subtle);}
 main{display:flex;flex-direction:column;flex:1;min-width:0;}.project-header{padding:24px 32px 0;border-bottom:1px solid var(--line);flex-shrink:0;}.project-title{display:flex;align-items:center;gap:10px;color:var(--subtle);min-height:30px;}.project-title h1{font-size:19px;font-weight:600;letter-spacing:-.4px;margin:0;color:var(--text);}.project-title a{display:flex;align-items:center;gap:5px;margin-left:auto;text-decoration:none;color:var(--subtle);font-size:12px;}.project-title a:hover{color:var(--accent);}.tabs{display:flex;gap:28px;margin-top:18px;}.tabs button{display:flex;gap:8px;align-items:center;position:relative;padding:12px 0;border:0;border-bottom:2px solid transparent;background:transparent;color:var(--muted);font-size:13px;}.tabs button.active{color:var(--text);border-bottom-color:var(--accent);font-weight:600;}.tabs span{font-size:11px;color:var(--subtle);font-weight:400;}
 .workspace{display:flex;flex-direction:column;flex:1;min-height:0;overflow:auto;}.conversation-space{overflow:hidden;}.context-space{padding:28px 32px;}.task-search{padding:24px 32px 0;max-width:1100px;width:100%;margin:auto;}.task-search label{display:flex;align-items:center;gap:8px;color:var(--subtle);max-width:320px;}.task-search input{border:0;background:transparent;padding:8px 0;color:var(--text);font-size:13px;min-width:0;width:100%;}.task-search button{border:0;background:transparent;color:var(--subtle);}
 .welcome{display:grid;flex:1;place-content:center;justify-items:center;padding:40px;text-align:center;color:var(--subtle);}.welcome h2{font-size:21px;letter-spacing:-.5px;color:var(--text);font-weight:500;}.welcome p{max-width:400px;font-size:14px;line-height:1.7;margin:0 0 24px;}.empty{color:var(--subtle);font-size:13px;padding:24px;}.primary{display:inline-flex;align-items:center;justify-content:center;gap:7px;padding:10px 16px;border:0;border-radius:6px;background:var(--accent);color:white;font-size:13px;font-weight:500;}
 .secondary-view{padding:28px 32px;}.secondary-view h2{font-size:20px;font-weight:600;}.secondary-view>p{font-size:13px;color:var(--subtle);}.result-row{display:flex;align-items:center;justify-content:space-between;gap:20px;width:100%;padding:18px 0;border:0;border-bottom:1px solid var(--line);background:transparent;text-align:left;color:var(--text);}.result-row strong{font-size:14px;font-weight:500;}.result-row span{font-size:12px;color:var(--muted);}
 .error{display:flex;align-items:center;justify-content:space-between;margin:12px 24px;padding:12px;color:var(--danger);background:var(--danger-soft);font-size:13px;border-radius:6px;}.error button{color:inherit;background:transparent;border:0;}
 .project-dialog{width:min(460px,calc(100% - 32px));padding:28px;border:1px solid var(--line);border-radius:12px;background:var(--panel);color:var(--text);box-shadow:var(--shadow);}.project-dialog::backdrop{background:var(--scrim);}.project-dialog header{display:flex;justify-content:space-between;align-items:center;}.project-dialog h2{font-size:19px;margin:0;}.project-dialog>p,.form-note{color:var(--subtle);font-size:13px;line-height:1.6;}.project-dialog form,.auth-card form{display:grid;gap:18px;margin-top:22px;}.project-dialog label,.auth-card label{display:grid;gap:8px;font-size:13px;}.project-dialog input,.auth-card input{width:100%;border:1px solid var(--line);border-radius:6px;background:var(--panel);color:var(--text);padding:10px;font:inherit;}.project-dialog .error{margin:0;}.auth-shell{display:grid;min-height:100dvh;place-items:center;background:var(--bg);}.auth-card{width:380px;max-width:calc(100vw - 32px);padding:32px;background:var(--panel);border:1px solid var(--line);border-radius:12px;}.auth-card h1{font-size:22px;}.auth-card p{font-size:13px;color:var(--muted);}
 .project-title .menu-button,.scrim{display:none;}
 @media(max-width:980px){.sidebar{position:fixed;inset:0 auto 0 0;z-index:40;transform:translateX(-100%);visibility:hidden;}.sidebar.mobile-open{transform:none;visibility:visible;}.scrim{display:block;position:fixed;inset:0;z-index:39;border:0;background:var(--scrim);}.project-title .menu-button{display:grid;}.project-header{padding:16px 20px 0;}.context-space,.secondary-view{padding:20px;}}
 @media(max-width:600px){.project-title a{display:none;}.task-search{padding:16px 16px 0;}.tabs{gap:24px;}}
</style>
