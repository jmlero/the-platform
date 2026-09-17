<script lang="ts">
  import { onMount } from 'svelte';
  import {
    ArrowRight,
    Blocks,
    Boxes,
    CheckCircle2,
    ChevronDown,
    CircleGauge,
    ClipboardList,
    Command,
    GitPullRequestArrow,
    Inbox,
    Layers3,
		LogOut,
    MessageSquare,
    Download,
    Upload,
    Menu,
    Plus,
    Search,
    Sparkles,
    Wifi,
    WifiOff,
    X
  } from '@lucide/svelte';
  import ActivityFeed from '$lib/components/ActivityFeed.svelte';
  import AgentStrip from '$lib/components/AgentStrip.svelte';
  import CreateTaskModal from '$lib/components/CreateTaskModal.svelte';
  import DecisionInbox from '$lib/components/DecisionInbox.svelte';
  import ChannelsView from '$lib/components/ChannelsView.svelte';
  import TaskCard from '$lib/components/TaskCard.svelte';
  import TaskDrawer from '$lib/components/TaskDrawer.svelte';
  import WorkOverview from '$lib/components/WorkOverview.svelte';
  import { workQueues } from '$lib/work-queues';
  import { api, subscribeToEvents } from '$lib/api';
	import type { OperatorSession } from '$lib/api';
  import { formatDate } from '$lib/format';
  import type {
    Agent,
    AppEvent,
    Channel,
    CreateTaskInput,
    Decision,
    Task,
    TaskDetail,
    TaskStatus,
    UpdateTaskInput,
    ProviderStatus
  } from '$lib/types';

  type View = 'overview' | 'factory' | 'backlog' | 'decisions' | 'channels';

  const boardColumns: Array<{
    status: TaskStatus;
    label: string;
    note: string;
  }> = [
    { status: 'ready', label: 'Ready', note: 'Available to claim' },
    { status: 'planning', label: 'Planning', note: 'Shaping an approach' },
    { status: 'waiting_decision', label: 'Decision', note: 'Human input needed' },
    { status: 'running', label: 'Running', note: 'Agent executing' },
    { status: 'verifying', label: 'Verifying', note: 'Checking evidence' },
    { status: 'done', label: 'Results', note: 'Inspect outcome and evidence' },
    { status: 'blocked', label: 'Attention', note: 'Needs intervention' }
  ];

  let activeView: View = 'overview';
  let tasks: Task[] = [];
  let agents: Agent[] = [];
  let decisions: Decision[] = [];
  let events: AppEvent[] = [];
  let channels: Channel[] = [];
  let channelRefreshVersion = 0;
  let providerStatus: ProviderStatus = {
    project_id: 'prj_default', connectivity: 'unknown', sync_state: 'unknown', sync_lag_millis: 0,
    lease_state: 'none', degraded_dependencies: [], stale_projection_count: 0, rejected_projection_count: 0
  };
  let selectedDetail: TaskDetail | null = null;
  let loading = true;
  let selectedLoading = false;
  let createOpen = false;
  let connected = false;
  let error = '';
  let search = '';
  let draggedTaskID = '';
  let dragTarget: TaskStatus | '' = '';
  let sidebarOpen = false;
  let refreshTimer: ReturnType<typeof setTimeout> | undefined;
	let session: OperatorSession | null = null;
	let sessionChecking = true;
	let loginToken = '';
	let loginError = '';
	let authenticating = false;
	let unsubscribeLiveEvents: (() => void) | undefined;

  $: normalizedSearch = search.trim().toLowerCase();
  $: filteredTasks = normalizedSearch
    ? tasks.filter((task) =>
        `${task.title} ${task.description} ${task.acceptance_criteria}`
          .toLowerCase()
          .includes(normalizedSearch)
      )
    : tasks;
  $: backlogTasks = filteredTasks.filter((task) => task.status === 'backlog');
  $: activeTasks = tasks.filter((task) =>
    ['planning', 'waiting_decision', 'running', 'verifying'].includes(task.status)
  );
  $: queues = workQueues(tasks);

  onMount(() => {
		const handleUnauthenticated = () => resetAuthenticatedWorkspace();
		window.addEventListener('appdeck:unauthenticated', handleUnauthenticated);
		void initializeSession();
    return () => {
			window.removeEventListener('appdeck:unauthenticated', handleUnauthenticated);
			unsubscribeLiveEvents?.();
      if (refreshTimer) clearTimeout(refreshTimer);
    };
  });

	async function initializeSession() {
		try {
			session = await api.session();
			startLiveUpdates();
			await loadWorkspace();
		} catch (caught) {
			if (!session) {
				loginError = '';
			} else {
				error = caught instanceof Error ? caught.message : 'Could not load AppDeck.';
			}
		} finally {
			sessionChecking = false;
			loading = false;
		}
	}

	async function login() {
		authenticating = true;
		loginError = '';
		try {
			session = await api.login(loginToken);
			loginToken = '';
			startLiveUpdates();
			await loadWorkspace();
		} catch (caught) {
			loginError = caught instanceof Error ? caught.message : 'Could not sign in.';
		} finally {
			authenticating = false;
		}
	}

	async function logout() {
		try {
			await api.logout();
		} finally {
			resetAuthenticatedWorkspace();
		}
	}

	function startLiveUpdates() {
		unsubscribeLiveEvents?.();
		unsubscribeLiveEvents = subscribeToEvents(handleLiveEvent, (state) => {
      connected = state;
      if (state) { channelRefreshVersion++; void loadWorkspace({ quiet: true }); }
    });
	}

	function resetAuthenticatedWorkspace() {
		session = null;
		connected = false;
		loading = false;
		tasks = [];
		agents = [];
		decisions = [];
		events = [];
		channels = [];
		selectedDetail = null;
		unsubscribeLiveEvents?.();
		unsubscribeLiveEvents = undefined;
	}

  async function loadWorkspace(options: { quiet?: boolean } = {}) {
    if (!options.quiet) loading = true;
    try {
      const snapshot = await api.bootstrap();
      tasks = snapshot.tasks;
      agents = snapshot.agents;
      decisions = snapshot.decisions;
      events = snapshot.events;
      channels = snapshot.channels;
      providerStatus = snapshot.provider_status;
      error = '';
    } catch (caught) {
      error = caught instanceof Error ? caught.message : 'Could not load AppDeck.';
    } finally {
      loading = false;
    }
  }

  function handleLiveEvent(event: AppEvent) {
    if (event.type === 'channel.message.created' || event.type === 'task.promoted_from_channel') channelRefreshVersion++;
    connected = true;
    if (event.id > 0) {
      events = [event, ...events.filter((candidate) => candidate.id !== event.id)].slice(0, 80);
      if (refreshTimer) clearTimeout(refreshTimer);
      refreshTimer = setTimeout(async () => {
        await loadWorkspace({ quiet: true });
        if (selectedDetail && event.task_id === selectedDetail.task.id) {
          await refreshSelected();
        }
      }, 100);
    }
  }

  async function openTask(task: Task) {
    selectedLoading = true;
    try {
      selectedDetail = await api.task(task.id);
      error = '';
    } catch (caught) {
      error = caught instanceof Error ? caught.message : 'Could not open the task.';
    } finally {
      selectedLoading = false;
    }
  }

  async function refreshSelected() {
    if (!selectedDetail) return;
    selectedDetail = await api.task(selectedDetail.task.id);
  }

  async function createTask(input: CreateTaskInput) {
    const created = await api.createTask(input);
    createOpen = false;
    await loadWorkspace({ quiet: true });
    await openTask(created);
  }

  async function moveTask(task: Task, status: TaskStatus) {
    await api.moveTask(task.id, status);
    await loadWorkspace({ quiet: true });
  }

  async function saveTask(task: Task, input: UpdateTaskInput) {
    await api.updateTask(task.id, input);
    await loadWorkspace({ quiet: true });
  }

  async function answerDecision(decision: Decision, answer: string) {
    await api.answerDecision(decision.id, answer);
    await loadWorkspace({ quiet: true });
  }

  async function handlePromotedTask(task: Task) {
    await loadWorkspace({ quiet: true });
    await openTask(task);
  }

  async function importProject(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    try {
      await api.importProject(await file.text());
      await loadWorkspace({ quiet: true });
    } catch (caught) {
      error = caught instanceof Error ? caught.message : 'Could not import the project ledger.';
    } finally {
      input.value = '';
    }
  }

  function startDrag(event: DragEvent, task: Task) {
    draggedTaskID = task.id;
    event.dataTransfer?.setData('text/plain', task.id);
    if (event.dataTransfer) event.dataTransfer.effectAllowed = 'move';
  }

  async function dropTask(target: TaskStatus) {
    dragTarget = '';
    const id = draggedTaskID;
    draggedTaskID = '';
    if (!id || target !== 'ready') return;
    const task = tasks.find((candidate) => candidate.id === id);
    if (!task || !['blocked', 'failed', 'cancelled', 'done', 'backlog'].includes(task.status)) return;
    try {
      await moveTask(task, target);
    } catch (caught) {
      error = caught instanceof Error ? caught.message : 'Could not move the task.';
    }
  }

  function tasksFor(status: TaskStatus): Task[] {
    if (status === 'blocked') {
      return filteredTasks.filter((task) => ['blocked', 'failed'].includes(task.status));
    }
    return filteredTasks.filter((task) => task.status === status);
  }

  function agentFor(task: Task): Agent | undefined {
    return agents.find((agent) => agent.id === task.assigned_agent_id);
  }

  function selectView(view: View) {
    activeView = view;
    sidebarOpen = false;
  }
</script>

<svelte:head>
  <title>AppDeck · Agent control plane</title>
</svelte:head>

{#if sessionChecking}
	<div class="auth-shell" aria-busy="true">
		<div class="auth-loading"><span></span><span>Checking operator session…</span></div>
	</div>
{:else if !session}
	<div class="auth-shell">
		<section class="auth-card" aria-labelledby="operator-login-title">
			<div class="brand auth-brand">
				<span class="brand-mark" aria-hidden="true"><i></i><i></i><i></i></span>
				<div><strong>AppDeck</strong><small>CONTROL PLANE</small></div>
			</div>
			<span class="eyebrow">Operator access</span>
			<h1 id="operator-login-title">Sign in to the control room</h1>
			<p>Enter the high-entropy operator token issued for this installation.</p>
			<form on:submit|preventDefault={login}>
				<label for="operator-token">Operator token</label>
				<input
					id="operator-token"
					type="password"
					bind:value={loginToken}
					autocomplete="current-password"
					required
				/>
				{#if loginError}<div class="auth-error" role="alert">{loginError}</div>{/if}
				<button type="submit" disabled={authenticating || loginToken.length < 32}>
					{authenticating ? 'Signing in…' : 'Enter AppDeck'} <ArrowRight size={15} />
				</button>
			</form>
			<small class="auth-note">The token is exchanged for a revocable 12-hour browser session.</small>
		</section>
	</div>
{:else}
<div class="app-shell">
  {#if sidebarOpen}
    <button class="mobile-scrim" type="button" on:click={() => (sidebarOpen = false)} aria-label="Close menu"></button>
  {/if}

  <aside class:mobile-open={sidebarOpen} class="sidebar">
    <div class="brand">
      <span class="brand-mark" aria-hidden="true"><i></i><i></i><i></i></span>
      <div><strong>AppDeck</strong><small>CONTROL PLANE</small></div>
    </div>

    <button class="workspace-switcher" type="button">
      <span class="workspace-icon"><Layers3 size={15} /></span>
      <span><small>Workspace</small><strong>AppDeck</strong></span>
      <ChevronDown size={14} />
    </button>

    <nav aria-label="Workspace navigation">
      <span class="nav-label">Work</span>
      <button class:active={activeView === 'overview'} type="button" on:click={() => selectView('overview')}>
        <CircleGauge size={17} /><span>Overview</span>
        {#if queues.attention.length}<em class="attention-count">{queues.attention.length}</em>{/if}
      </button>
      <button class:active={activeView === 'factory'} type="button" on:click={() => selectView('factory')}>
        <Boxes size={17} />
        <span>Task board</span>
        {#if activeTasks.length}<em>{activeTasks.length}</em>{/if}
      </button>
      <button class:active={activeView === 'backlog'} type="button" on:click={() => selectView('backlog')}>
        <ClipboardList size={17} />
        <span>Backlog</span>
        {#if tasks.filter((task) => task.status === 'backlog').length}
          <em>{tasks.filter((task) => task.status === 'backlog').length}</em>
        {/if}
      </button>
      <button class:active={activeView === 'decisions'} type="button" on:click={() => selectView('decisions')}>
        <GitPullRequestArrow size={17} />
        <span>Decisions</span>
        {#if decisions.length}<em class="attention-count">{decisions.length}</em>{/if}
      </button>
      <span class="nav-label secondary-label">Supporting context</span>
      <button class:active={activeView === 'channels'} type="button" on:click={() => selectView('channels')}>
        <MessageSquare size={17} />
        <span>Channels</span>
        {#if channels.length}<em>{channels.length}</em>{/if}
      </button>
    </nav>

    <div class="sidebar-bottom">
      <div class="system-card">
        <div class="system-top">
          <span><CircleGauge size={14} /> Provider {providerStatus.connectivity}</span>
          <i class:offline={providerStatus.connectivity !== 'connected'}></i>
        </div>
        <div class="system-stats">
          <span><strong>{providerStatus.sync_state}</strong> sync</span>
          <span><strong>{providerStatus.lease_state}</strong> lease</span>
        </div>
        {#if providerStatus.rejected_projection_count || providerStatus.degraded_dependencies.length}
          <small class="system-warning">{providerStatus.rejected_projection_count} rejected · {providerStatus.degraded_dependencies.join(', ') || 'no dependency detail'}</small>
        {/if}
      </div>
      <div class="version"><Command size={12} /> Prototype 0.1</div>
    </div>
  </aside>

  <main>
    <header class="topbar">
      <button class="menu-button" type="button" on:click={() => (sidebarOpen = true)} aria-label="Open menu">
        <Menu size={18} />
      </button>
      <div class="location">
        <span>AppDeck</span><span>/</span><strong>{activeView === 'factory' ? 'Task board' : activeView}</strong>
      </div>
      <div class="topbar-actions">
        <label class="search-box">
          <Search size={15} />
          <input bind:value={search} placeholder="Search work…" />
          {#if search}
            <button type="button" on:click={() => (search = '')} aria-label="Clear search"><X size={13} /></button>
          {:else}
            <kbd>⌘ K</kbd>
          {/if}
        </label>
        <span class:offline={!connected} class="connection">
          {#if connected}<Wifi size={13} /> Live{:else}<WifiOff size={13} /> Reconnecting{/if}
        </span>
				<a class="export-link" href={api.exportURL()} download="appdeck-export.json" title="Export deterministic project ledger"><Download size={15} /></a>
				<label class="export-link import-link" title="Import into an empty project ledger"><Upload size={15} /><input type="file" accept="application/json,.json" on:change={importProject} /></label>
				{#if session.expires_at}
					<span class="operator-name">{session.operator_id}</span>
					<button class="sign-out" type="button" on:click={logout} aria-label="Sign out" title="Sign out">
						<LogOut size={15} />
					</button>
				{/if}
        <button class="new-task" type="button" on:click={() => (createOpen = true)}>
          <Plus size={15} strokeWidth={2.5} /> New task
        </button>
      </div>
    </header>

    <section class="workspace">
      <header class="page-heading">
        <div>
          <span class="eyebrow">
            {activeView === 'overview' ? 'Your work' : activeView === 'factory' ? 'Work status' : activeView === 'backlog' ? 'Work inventory' : activeView === 'channels' ? 'Project conversation' : 'Human attention'}
          </span>
          <h1>
            {activeView === 'overview' ? 'Decisions and results' : activeView === 'factory' ? 'Task board' : activeView === 'backlog' ? 'Backlog' : activeView === 'channels' ? 'Channels' : 'Decision inbox'}
          </h1>
          <p>
            {activeView === 'overview' ? 'Authorize bounded work, resolve exceptions, and inspect what changed.' : activeView === 'factory'
              ? 'Observe, steer, and verify every active unit of work.'
              : activeView === 'backlog'
                ? 'Define an outcome and its limits before authorizing execution.'
                : activeView === 'channels'
                  ? 'Converse with agents, cite canonical records, and promote messages into bounded work.'
                  : 'Resolve the questions currently blocking execution.'}
          </p>
        </div>
        <div class="headline-stats">
          <div><span>In progress</span><strong>{queues.active.length}</strong></div>
          <div><span>Needs you</span><strong class:has-attention={queues.attention.length > 0}>{queues.attention.length}</strong></div>
          <div><span>Results</span><strong>{queues.results.length}</strong></div>
        </div>
      </header>

      {#if error}
        <div class="error-banner">
          <span>{error}</span>
          <button type="button" on:click={() => (error = '')}><X size={14} /></button>
        </div>
      {/if}

      {#if loading}
        <div class="loading-layout">
          <div class="skeleton loading-strip"></div>
          <div class="loading-columns">
            {#each Array(4) as _}<div class="skeleton"></div>{/each}
          </div>
        </div>
      {:else if activeView === 'overview'}
        <WorkOverview tasks={filteredTasks} {agents} onselect={openTask} />
      {:else if activeView === 'factory'}
        <div class="factory-stack">
          <AgentStrip {agents} {tasks} onselect={openTask} />
          <div class="factory-body">
            <section class="board" aria-label="Task board">
              {#each boardColumns as column (column.status)}
                {@const columnTasks = tasksFor(column.status)}
                <div
                  class="board-column column-{column.status}"
                  role="list"
                  class:drop-target={dragTarget === column.status && column.status === 'ready'}
                  on:dragover={(event) => {
                    if (column.status === 'ready') {
                      event.preventDefault();
                      dragTarget = column.status;
                    }
                  }}
                  on:dragleave={() => (dragTarget = '')}
                  on:drop|preventDefault={() => dropTask(column.status)}
                >
                  <header>
                    <div><span class="column-signal"></span><strong>{column.label}</strong><em>{columnTasks.length}</em></div>
                    <small>{column.note}</small>
                  </header>
                  <div class="column-cards">
                    {#each columnTasks as task (task.id)}
                      <TaskCard
                        {task}
                        agent={agentFor(task)}
                        draggable={['blocked', 'failed', 'done'].includes(task.status)}
                        onselect={openTask}
                        ondrag={startDrag}
                      />
                    {:else}
                      <div class="empty-column">
                        {#if column.status === 'ready'}<Inbox size={18} />{:else}<Blocks size={18} />{/if}
                        <span>{normalizedSearch ? 'No matching work' : 'No work here'}</span>
                      </div>
                    {/each}
                  </div>
                </div>
              {/each}
            </section>
            <ActivityFeed {events} {tasks} {agents} onselect={openTask} />
          </div>
        </div>
      {:else if activeView === 'backlog'}
        <div class="backlog-layout">
          <section class="backlog-panel glass">
            <header class="list-header">
              <div>
                <span class="eyebrow">Unscheduled work</span>
                <h2>{backlogTasks.length} outcomes to shape</h2>
              </div>
              <button type="button" on:click={() => (createOpen = true)}><Plus size={14} /> Add outcome</button>
            </header>
            <div class="backlog-list">
              {#each backlogTasks as task (task.id)}
                <article>
                  <span class="backlog-priority priority-{task.priority}"><i></i>{task.priority}</span>
                  <button class="backlog-copy" type="button" on:click={() => openTask(task)}>
                    <strong>{task.title}</strong>
                    <span class="clamp-2">{task.acceptance_criteria || task.description || 'Acceptance criteria not defined yet.'}</span>
                  </button>
                  <span class="backlog-budget">{task.budget_minutes}m</span>
                  <span class="backlog-date">{formatDate(task.created_at)}</span>
                  <button class="promote" type="button" on:click={() => moveTask(task, 'ready').catch((caught) => { error = caught instanceof Error ? caught.message : 'Could not move the task.'; })} title="Move to ready queue">
                    Ready <ArrowRight size={14} />
                  </button>
                </article>
              {:else}
                <div class="backlog-empty">
                  <CheckCircle2 size={25} />
                  <h3>{normalizedSearch ? 'No matching backlog tasks' : 'The backlog is clear'}</h3>
                  <p>{normalizedSearch ? 'Try a different search.' : 'Capture a new outcome when you are ready.'}</p>
                </div>
              {/each}
            </div>
          </section>
          <aside class="backlog-guide glass">
            <span class="guide-icon"><Sparkles size={18} /></span>
            <span class="eyebrow">Ready-work checklist</span>
            <h3>Give agents a strong contract</h3>
            <ul>
              <li><CheckCircle2 size={14} /> One observable outcome</li>
              <li><CheckCircle2 size={14} /> Explicit constraints</li>
              <li><CheckCircle2 size={14} /> Verifiable acceptance criteria</li>
              <li><CheckCircle2 size={14} /> Time and permission budget</li>
            </ul>
          </aside>
        </div>
      {:else if activeView === 'channels'}
        <ChannelsView {channels} refreshVersion={channelRefreshVersion} onpromoted={handlePromotedTask} />
      {:else}
        <DecisionInbox {decisions} {tasks} onanswer={answerDecision} onselect={openTask} />
      {/if}
    </section>
  </main>

  {#if selectedLoading}
    <div class="detail-loading"><span></span></div>
  {/if}

  {#if selectedDetail}
    <TaskDrawer
      detail={selectedDetail}
      {agents}
      onclose={() => (selectedDetail = null)}
      onrefresh={refreshSelected}
      onmove={moveTask}
      onsave={saveTask}
      onanswer={answerDecision}
    />
  {/if}

  {#if createOpen}
    <CreateTaskModal onclose={() => (createOpen = false)} oncreate={createTask} />
  {/if}
</div>
{/if}

<style>
	.auth-shell {
		display: grid;
		width: 100vw;
		height: 100vh;
		padding: 24px;
		background:
			radial-gradient(circle at 50% 20%, rgba(184, 243, 107, 0.08), transparent 30%),
			var(--bg);
		place-items: center;
	}

	.auth-card {
		width: min(420px, 100%);
		padding: 34px;
		background: var(--panel);
		border: 1px solid var(--line-strong);
		border-radius: 18px;
		box-shadow: var(--shadow);
	}

	.auth-brand { margin: 0 0 42px; padding: 0; }
	.auth-card > .eyebrow { color: var(--acid); }
	.auth-card h1 { margin: 9px 0 10px; font-size: 1.65rem; letter-spacing: -0.04em; }
	.auth-card > p { margin: 0; color: var(--muted); font-size: 0.84rem; line-height: 1.6; }
	.auth-card form { display: grid; gap: 10px; margin-top: 28px; }
	.auth-card label { color: var(--muted); font-size: 0.68rem; font-weight: 700; }
	.auth-card input {
		width: 100%;
		height: 45px;
		padding: 0 12px;
		color: var(--text);
		background: var(--bg);
		border: 1px solid var(--line-strong);
		border-radius: 9px;
		font-family: "SFMono-Regular", Consolas, monospace;
	}
	.auth-card form button {
		display: inline-flex;
		height: 43px;
		align-items: center;
		justify-content: center;
		gap: 8px;
		margin-top: 5px;
		color: #11150d;
		font-size: 0.74rem;
		font-weight: 750;
		background: var(--acid);
		border: 0;
		border-radius: 9px;
		cursor: pointer;
	}
	.auth-card form button:disabled { cursor: not-allowed; opacity: 0.45; }
	.auth-error { color: var(--red); font-size: 0.7rem; }
	.auth-note { display: block; margin-top: 20px; color: var(--subtle); font-size: 0.62rem; line-height: 1.5; }
	.auth-loading { display: inline-flex; align-items: center; gap: 10px; color: var(--muted); font-size: 0.75rem; }
	.auth-loading span:first-child { width: 9px; height: 9px; background: var(--acid); border-radius: 50%; animation: pulse-ring 1.2s infinite; }
	.operator-name { max-width: 130px; overflow: hidden; color: var(--subtle); font-size: 0.62rem; text-overflow: ellipsis; white-space: nowrap; }
	.sign-out {
		display: grid;
		width: 32px;
		height: 32px;
		color: var(--muted);
		background: transparent;
		border: 1px solid var(--line);
		border-radius: 8px;
		cursor: pointer;
		place-items: center;
	}
	.sign-out:hover { color: var(--text); border-color: var(--line-strong); }

  .app-shell {
    display: flex;
    width: 100vw;
    height: 100vh;
    overflow: hidden;
    background:
      radial-gradient(circle at 55% -20%, rgba(184, 243, 107, 0.045), transparent 34%),
      var(--bg);
  }

  .sidebar {
    z-index: 30;
    display: flex;
    width: 220px;
    flex: 0 0 auto;
    flex-direction: column;
    padding: 19px 13px 13px;
    background: var(--sidebar);
    border-right: 1px solid var(--line);
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 0 7px;
  }

  .brand-mark {
    position: relative;
    display: block;
    width: 29px;
    height: 29px;
    transform: rotate(-4deg);
  }

  .brand-mark i {
    position: absolute;
    display: block;
    width: 19px;
    height: 19px;
    border: 2px solid var(--acid);
    border-radius: 6px;
  }

  .brand-mark i:nth-child(1) { top: 0; left: 0; opacity: 0.4; }
  .brand-mark i:nth-child(2) { top: 5px; left: 5px; opacity: 0.7; }
  .brand-mark i:nth-child(3) { top: 10px; left: 10px; background: var(--acid); }

  .brand strong,
  .brand small {
    display: block;
  }

  .brand strong {
    font-size: 0.92rem;
    font-weight: 750;
    letter-spacing: -0.02em;
  }

  .brand small {
    margin-top: 2px;
    color: var(--subtle);
    font-family: "SFMono-Regular", Consolas, monospace;
    font-size: 0.48rem;
    letter-spacing: 0.13em;
  }

  .workspace-switcher {
    display: flex;
    width: 100%;
    min-height: 49px;
    align-items: center;
    gap: 9px;
    margin-top: 25px;
    padding: 7px 9px;
    text-align: left;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--line);
    border-radius: 11px;
    cursor: pointer;
  }

  .workspace-icon {
    display: grid;
    width: 29px;
    height: 29px;
    color: var(--acid);
    background: var(--acid-soft);
    border-radius: 8px;
    place-items: center;
  }

  .workspace-switcher > span:nth-child(2) { min-width: 0; flex: 1; }
  .workspace-switcher small, .workspace-switcher strong { display: block; }
  .workspace-switcher small { color: var(--subtle); font-size: 0.53rem; }
  .workspace-switcher strong { margin-top: 2px; font-size: 0.69rem; font-weight: 650; }
  .workspace-switcher > :global(svg) { color: var(--subtle); }

  nav {
    display: grid;
    gap: 3px;
    margin-top: 23px;
  }

  .nav-label {
    margin: 0 9px 7px;
    color: #545c5b;
    font-size: 0.55rem;
    font-weight: 750;
    letter-spacing: 0.12em;
    text-transform: uppercase;
  }

  .secondary-label { margin-top: 22px; }

  nav button {
    position: relative;
    display: flex;
    width: 100%;
    height: 39px;
    align-items: center;
    gap: 10px;
    padding: 0 10px;
    color: #7c8583;
    font-size: 0.7rem;
    font-weight: 590;
    text-align: left;
    background: transparent;
    border: 0;
    border-radius: 9px;
    cursor: pointer;
  }

  nav button:hover:not(:disabled) { color: var(--text); background: rgba(255, 255, 255, 0.035); }
  nav button.active { color: var(--text); background: rgba(255, 255, 255, 0.055); }
  nav button.active::before { position: absolute; top: 9px; bottom: 9px; left: -13px; width: 2px; background: var(--acid); border-radius: 2px; content: ""; }
  nav button.active > :global(svg) { color: var(--acid); }
  nav button span { flex: 1; }
  nav button em { min-width: 20px; padding: 2px 5px; color: var(--subtle); font-size: 0.55rem; font-style: normal; text-align: center; background: rgba(255, 255, 255, 0.045); border-radius: 5px; }
  nav button em.attention-count { color: var(--amber); background: var(--amber-soft); }
  nav button:disabled { cursor: default; opacity: 0.52; }

  .sidebar-bottom { margin-top: auto; }

  .system-card {
    padding: 11px;
    background: rgba(255, 255, 255, 0.025);
    border: 1px solid var(--line);
    border-radius: 11px;
  }

  .system-top { display: flex; align-items: center; justify-content: space-between; color: var(--muted); font-size: 0.61rem; }
  .system-top span { display: inline-flex; align-items: center; gap: 6px; }
  .system-top i { width: 6px; height: 6px; background: var(--acid); border-radius: 999px; box-shadow: 0 0 0 4px var(--acid-soft); }
  .system-top i.offline { background: var(--amber); box-shadow: 0 0 0 4px var(--amber-soft); }
  .system-stats { display: flex; gap: 16px; margin-top: 12px; color: var(--subtle); font-size: 0.56rem; }
  .system-stats strong { display: block; margin-bottom: 2px; color: var(--text); font-size: 0.7rem; }
  .system-warning { display: block; margin-top: 9px; color: var(--amber); font-size: 0.54rem; line-height: 1.4; }
  .version { display: flex; align-items: center; gap: 6px; margin: 10px 4px 0; color: #4e5654; font-family: "SFMono-Regular", Consolas, monospace; font-size: 0.52rem; }

  main { display: flex; min-width: 0; flex: 1; flex-direction: column; }

  .topbar {
    display: flex;
    height: 58px;
    flex: 0 0 auto;
    align-items: center;
    justify-content: space-between;
    padding: 0 19px 0 23px;
    background: rgba(10, 12, 15, 0.88);
    border-bottom: 1px solid var(--line);
    backdrop-filter: blur(15px);
  }

  .location { display: flex; align-items: center; gap: 7px; color: var(--subtle); font-size: 0.66rem; text-transform: capitalize; }
  .location strong { color: var(--muted); font-weight: 600; }
  .topbar-actions { display: flex; align-items: center; gap: 9px; }

  .search-box {
    display: flex;
    width: 210px;
    height: 34px;
    align-items: center;
    gap: 7px;
    padding: 0 8px 0 10px;
    color: var(--subtle);
    background: rgba(255, 255, 255, 0.025);
    border: 1px solid var(--line);
    border-radius: 9px;
  }

  .search-box input { min-width: 0; flex: 1; color: var(--text); font-size: 0.66rem; background: transparent; border: 0; outline: 0; }
  .search-box input::placeholder { color: var(--subtle); }
  .search-box kbd { padding: 2px 5px; color: #5a6260; font-family: inherit; font-size: 0.52rem; background: rgba(255, 255, 255, 0.04); border: 1px solid var(--line); border-radius: 4px; }
  .search-box button { display: grid; width: 19px; height: 19px; color: var(--subtle); background: transparent; border: 0; cursor: pointer; place-items: center; }

  .connection { display: inline-flex; height: 30px; align-items: center; gap: 6px; padding: 0 9px; color: var(--acid); font-size: 0.61rem; font-weight: 700; background: var(--acid-soft); border-radius: 8px; }
  .connection.offline { color: var(--amber); background: var(--amber-soft); }
  .export-link { display: grid; width: 32px; height: 32px; color: var(--muted); border: 1px solid var(--line); border-radius: 8px; place-items: center; }
  .export-link:hover { color: var(--text); border-color: var(--line-strong); }
  .import-link { cursor: pointer; }
  .import-link input { position: absolute; width: 1px; height: 1px; opacity: 0; }

  .new-task,
  .list-header button {
    display: inline-flex;
    height: 34px;
    align-items: center;
    gap: 6px;
    padding: 0 12px;
    color: #10140c;
    font-size: 0.66rem;
    font-weight: 750;
    background: var(--acid);
    border: 0;
    border-radius: 9px;
    cursor: pointer;
  }

  .menu-button { display: none; }

  .workspace {
    display: flex;
    min-height: 0;
    flex: 1;
    flex-direction: column;
    padding: 20px 20px 16px;
    overflow: hidden;
  }

  .page-heading { display: flex; flex: 0 0 auto; align-items: flex-end; justify-content: space-between; padding: 0 2px 17px; }
  .page-heading h1 { margin: 4px 0 3px; font-size: 1.55rem; font-weight: 660; letter-spacing: -0.04em; }
  .page-heading p { margin: 0; color: var(--muted); font-size: 0.69rem; }

  .headline-stats { display: flex; align-items: center; }
  .headline-stats > div { min-width: 83px; padding: 0 15px; border-left: 1px solid var(--line); }
  .headline-stats span, .headline-stats strong { display: block; text-align: right; }
  .headline-stats span { color: var(--subtle); font-size: 0.57rem; text-transform: uppercase; letter-spacing: 0.06em; }
  .headline-stats strong { margin-top: 3px; font-size: 0.88rem; font-weight: 670; }
  .headline-stats strong.has-attention { color: var(--amber); }

  .error-banner { display: flex; flex: 0 0 auto; align-items: center; justify-content: space-between; margin-bottom: 10px; padding: 9px 11px; color: var(--red); font-size: 0.68rem; background: var(--red-soft); border: 1px solid rgba(255, 120, 111, 0.14); border-radius: 9px; }
  .error-banner button { display: grid; width: 22px; height: 22px; color: inherit; background: transparent; border: 0; cursor: pointer; place-items: center; }

  .factory-stack { display: flex; min-height: 0; flex: 1; flex-direction: column; gap: 11px; }
  .factory-body { display: flex; min-height: 0; flex: 1; gap: 11px; }

  .board { display: flex; min-width: 0; flex: 1; gap: 10px; padding: 1px 1px 9px; overflow-x: auto; overflow-y: hidden; scroll-snap-type: x proximity; }

  .board-column {
    display: flex;
    width: 248px;
    min-width: 248px;
    height: 100%;
    flex-direction: column;
    background: rgba(255, 255, 255, 0.018);
    border: 1px solid var(--line);
    border-radius: 14px;
    scroll-snap-align: start;
    transition: border-color 150ms ease, background 150ms ease;
  }

  .board-column.drop-target { background: var(--acid-soft); border-color: rgba(184, 243, 107, 0.35); }
  .board-column > header { flex: 0 0 auto; padding: 13px 12px 11px; border-bottom: 1px solid var(--line); }
  .board-column > header > div { display: flex; align-items: center; gap: 7px; }
  .board-column > header strong { font-size: 0.7rem; font-weight: 680; }
  .board-column > header em { min-width: 19px; padding: 2px 5px; color: var(--subtle); font-size: 0.54rem; font-style: normal; text-align: center; background: rgba(255, 255, 255, 0.045); border-radius: 5px; }
  .board-column > header small { display: block; margin: 4px 0 0 15px; color: var(--subtle); font-size: 0.57rem; }
  .column-signal { width: 7px; height: 7px; background: var(--cyan); border-radius: 999px; }
  .column-planning .column-signal { background: var(--violet); }
  .column-waiting_decision .column-signal { background: var(--amber); box-shadow: 0 0 0 4px var(--amber-soft); }
  .column-running .column-signal, .column-done .column-signal { background: var(--acid); }
  .column-verifying .column-signal { background: var(--cyan); box-shadow: 0 0 0 4px var(--cyan-soft); }
  .column-blocked .column-signal { background: var(--red); }

  .column-cards { display: flex; min-height: 0; flex: 1; flex-direction: column; gap: 8px; padding: 9px; overflow-y: auto; }
  .empty-column { display: grid; min-height: 115px; flex: 0 0 auto; place-content: center; justify-items: center; gap: 8px; color: #4e5755; font-size: 0.62rem; border: 1px dashed rgba(255, 255, 255, 0.065); border-radius: 11px; }

  .loading-layout { display: flex; min-height: 0; flex: 1; flex-direction: column; gap: 11px; }
  .loading-strip { height: 86px; flex: 0 0 auto; border-radius: 16px; }
  .loading-columns { display: grid; min-height: 0; flex: 1; grid-template-columns: repeat(4, 1fr); gap: 10px; }
  .loading-columns > div { border-radius: 14px; }

  .backlog-layout { display: grid; min-height: 0; flex: 1; grid-template-columns: minmax(0, 1fr) 270px; gap: 11px; }
  .backlog-panel { min-height: 0; overflow: hidden; border-radius: 15px; }
  .list-header { display: flex; min-height: 70px; align-items: center; justify-content: space-between; padding: 13px 16px; border-bottom: 1px solid var(--line); }
  .list-header h2 { margin: 4px 0 0; font-size: 0.88rem; font-weight: 660; }
  .list-header button { color: var(--text); background: rgba(255, 255, 255, 0.055); border: 1px solid var(--line); }
  .backlog-list { height: calc(100% - 70px); overflow-y: auto; }
  .backlog-list article { display: grid; min-height: 76px; grid-template-columns: 72px minmax(220px, 1fr) 55px 100px 78px; align-items: center; gap: 12px; padding: 10px 15px; border-bottom: 1px solid var(--line); transition: background 130ms ease; }
  .backlog-list article:hover { background: rgba(255, 255, 255, 0.02); }
  .backlog-priority { display: inline-flex; align-items: center; gap: 6px; color: var(--cyan); font-size: 0.58rem; font-weight: 750; text-transform: uppercase; }
  .backlog-priority i { width: 6px; height: 6px; background: currentColor; border-radius: 999px; }
  .backlog-priority.priority-urgent { color: var(--red); }
  .backlog-priority.priority-high { color: var(--violet); }
  .backlog-priority.priority-low { color: var(--subtle); }
  .backlog-copy { min-width: 0; text-align: left; background: transparent; border: 0; cursor: pointer; }
  .backlog-copy strong, .backlog-copy span { display: block; }
  .backlog-copy strong { font-size: 0.75rem; font-weight: 650; }
  .backlog-copy span { margin-top: 5px; color: var(--muted); font-size: 0.64rem; line-height: 1.4; }
  .backlog-budget, .backlog-date { color: var(--subtle); font-size: 0.62rem; }
  .promote { display: inline-flex; height: 30px; align-items: center; justify-content: center; gap: 4px; color: var(--acid); font-size: 0.62rem; font-weight: 700; background: var(--acid-soft); border: 0; border-radius: 8px; cursor: pointer; }
  .backlog-empty { display: grid; min-height: 300px; place-content: center; justify-items: center; color: var(--subtle); text-align: center; }
  .backlog-empty h3 { margin: 10px 0 4px; color: var(--muted); font-size: 0.8rem; }
  .backlog-empty p { margin: 0; font-size: 0.65rem; }

  .backlog-guide { height: fit-content; padding: 17px; border-radius: 15px; }
  .guide-icon { display: grid; width: 39px; height: 39px; margin-bottom: 16px; color: var(--acid); background: var(--acid-soft); border-radius: 11px; place-items: center; }
  .backlog-guide h3 { margin: 7px 0 15px; font-size: 0.88rem; font-weight: 650; line-height: 1.35; }
  .backlog-guide ul { display: grid; gap: 10px; margin: 0; padding: 0; list-style: none; }
  .backlog-guide li { display: flex; align-items: center; gap: 7px; color: var(--muted); font-size: 0.66rem; }
  .backlog-guide li :global(svg) { color: var(--acid); }

  .detail-loading { position: fixed; z-index: 90; top: 68px; right: 18px; display: grid; width: 33px; height: 33px; background: var(--panel-raised); border: 1px solid var(--line); border-radius: 10px; box-shadow: var(--shadow); place-items: center; }
  .detail-loading span { width: 14px; height: 14px; border: 2px solid var(--line-strong); border-top-color: var(--acid); border-radius: 999px; animation: spin 700ms linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }

  .mobile-scrim { display: none; }

  @media (max-width: 980px) {
    .sidebar { position: fixed; top: 0; bottom: 0; left: 0; transform: translateX(-100%); transition: transform 180ms ease; }
    .sidebar.mobile-open { transform: translateX(0); }
    .mobile-scrim { position: fixed; z-index: 25; inset: 0; display: block; background: rgba(0, 0, 0, 0.52); border: 0; }
    .menu-button { display: grid; width: 32px; height: 32px; margin-right: 10px; color: var(--muted); background: transparent; border: 1px solid var(--line); border-radius: 8px; place-items: center; }
    .location { margin-right: auto; }
    .backlog-layout { grid-template-columns: 1fr; }
    .backlog-guide { display: none; }
  }

  @media (max-width: 760px) {
    .workspace { padding: 16px 12px 12px; }
    .topbar { padding: 0 12px; }
    .search-box { width: 150px; }
    .connection { display: none; }
    .page-heading { align-items: flex-start; }
    .headline-stats { display: none; }
    .backlog-list article { grid-template-columns: 64px 1fr 70px; }
    .backlog-date, .backlog-budget { display: none; }
    .promote { grid-column: 3; grid-row: 1; }
  }

  @media (max-width: 520px) {
    .location { display: none; }
    .search-box { flex: 1; width: auto; }
    .topbar-actions { min-width: 0; flex: 1; }
    .new-task { width: 34px; padding: 0; font-size: 0; justify-content: center; }
    .page-heading h1 { font-size: 1.35rem; }
    .backlog-list article { grid-template-columns: 1fr 64px; }
    .backlog-priority { display: none; }
    .promote { grid-column: 2; }
  }
</style>
