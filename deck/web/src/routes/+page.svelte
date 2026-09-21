<script lang="ts">
  import { onMount } from 'svelte';
  import {
    ArrowRight,
    Blocks,
    Boxes,
    CheckCircle2,
    CircleGauge,
    ClipboardList,
    Waves,
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
  import BrandMark from '$lib/components/BrandMark.svelte';
  import PageHeading from '$lib/components/PageHeading.svelte';
  import ActivityFeed from '$lib/components/ActivityFeed.svelte';
  import AgentStrip from '$lib/components/AgentStrip.svelte';
  import CreateTaskModal from '$lib/components/CreateTaskModal.svelte';
  import DecisionInbox from '$lib/components/DecisionInbox.svelte';
  import ChannelsView from '$lib/components/ChannelsView.svelte';
  import ProjectContext from '$lib/components/ProjectContext.svelte';
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

  type View = 'overview' | 'factory' | 'backlog' | 'decisions' | 'channels' | 'context';

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
		window.addEventListener('deck:unauthenticated', handleUnauthenticated);
		void initializeSession();
    return () => {
			window.removeEventListener('deck:unauthenticated', handleUnauthenticated);
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
				error = caught instanceof Error ? caught.message : 'Could not load Deck.';
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
      error = caught instanceof Error ? caught.message : 'Could not load Deck.';
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
  <title>Deck · Work, decisions, and results</title>
</svelte:head>

{#if sessionChecking}
	<div class="auth-shell" aria-busy="true">
		<div class="auth-loading"><span></span><span>Checking operator session…</span></div>
	</div>
{:else if !session}
	<div class="auth-shell">
		<section class="auth-card" aria-labelledby="operator-login-title">
			<div class="brand auth-brand">
				<BrandMark />
				<div><strong>Deck</strong><small>THE PLATFORM</small></div>
			</div>
			<span class="eyebrow">Operator access</span>
			<h1 id="operator-login-title">Sign in to the control room</h1>
			<p>Use your operator token to access your workspace.</p>
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
					{authenticating ? 'Signing in…' : 'Enter Deck'} <ArrowRight size={15} />
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

  <aside class:mobile-open={sidebarOpen} class="sidebar" id="workspace-navigation">
    <div class="brand">
      <BrandMark />
      <div><strong>Deck</strong><small>THE PLATFORM</small></div>
    </div>

    <div class="workspace-identity">
      <span class="workspace-icon"><Layers3 size={15} /></span>
      <span><small>Workspace</small><strong>Work control</strong></span>
      </div>

    <nav aria-label="Workspace navigation">
      <span class="nav-label">Work</span>
      <button class:active={activeView === 'overview'} aria-current={activeView === 'overview' ? 'page' : undefined} type="button" on:click={() => selectView('overview')}>
        <CircleGauge size={17} /><span>Overview</span>
        {#if queues.attention.length}<em class="attention-count">{queues.attention.length}</em>{/if}
      </button>
      <button class:active={activeView === 'factory'} aria-current={activeView === 'factory' ? 'page' : undefined} type="button" on:click={() => selectView('factory')}>
        <Boxes size={17} />
        <span>Task board</span>
        {#if activeTasks.length}<em>{activeTasks.length}</em>{/if}
      </button>
      <button class:active={activeView === 'backlog'} aria-current={activeView === 'backlog' ? 'page' : undefined} type="button" on:click={() => selectView('backlog')}>
        <ClipboardList size={17} />
        <span>Backlog</span>
        {#if tasks.filter((task) => task.status === 'backlog').length}
          <em>{tasks.filter((task) => task.status === 'backlog').length}</em>
        {/if}
      </button>
      <button class:active={activeView === 'decisions'} aria-current={activeView === 'decisions' ? 'page' : undefined} type="button" on:click={() => selectView('decisions')}>
        <GitPullRequestArrow size={17} />
        <span>Decisions</span>
        {#if decisions.length}<em class="attention-count">{decisions.length}</em>{/if}
      </button>
      <span class="nav-label secondary-label">Collaborate</span>
      <button class:active={activeView === 'channels'} aria-current={activeView === 'channels' ? 'page' : undefined} type="button" on:click={() => selectView('channels')}>
        <MessageSquare size={17} />
        <span>Channels</span>
        {#if channels.length}<em>{channels.length}</em>{/if}
      </button>
      <button class:active={activeView === 'context'} aria-current={activeView === 'context' ? 'page' : undefined} type="button" on:click={() => selectView('context')}>
        <Layers3 size={17} /><span>Project context</span>
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
      <div class="platform-signature"><Waves size={16} /><span><strong>Deck & Marina</strong><small>The Platform · v0.1</small></span></div>
    </div>
  </aside>

  <main>
    <header class="topbar">
      <button class="menu-button" type="button" on:click={() => (sidebarOpen = true)} aria-label="Open menu" aria-controls="workspace-navigation" aria-expanded={sidebarOpen}>
        <Menu size={18} />
      </button>
      <div class="location">
        <span>Deck</span><span>/</span><strong>{activeView === 'factory' ? 'Task board' : activeView}</strong>
      </div>
      <div class="topbar-actions">
        <label class="search-box">
          <Search size={15} />
          <input bind:value={search} placeholder="Search work…" aria-label="Search work" />
          {#if search}
            <button type="button" on:click={() => (search = '')} aria-label="Clear search"><X size={13} /></button>
          {/if}
        </label>
        <span class:offline={!connected} class="connection">
          {#if connected}<Wifi size={13} /> Live{:else}<WifiOff size={13} /> Reconnecting{/if}
        </span>
				<a class="export-link" href={api.exportURL()} download="deck-export.json" title="Export project" aria-label="Export project"><Download size={15} /></a>
				<label class="export-link import-link" title="Import into an empty project"><Upload size={15} /><input type="file" aria-label="Import project" accept="application/json,.json" on:change={importProject} /></label>
				{#if session.expires_at}
					<span class="operator-name">{session.operator_id}</span>
					<button class="sign-out" type="button" on:click={logout} aria-label="Sign out" title="Sign out">
						<LogOut size={15} />
					</button>
				{/if}
        <button class="new-task" aria-label="New task" type="button" on:click={() => (createOpen = true)}>
          <Plus size={15} strokeWidth={2.5} /> New task
        </button>
      </div>
    </header>

    <section class="workspace" aria-label="Workspace content">
      <PageHeading
        eyebrow={activeView === 'context' ? 'Project knowledge' : activeView === 'overview' ? 'Your workspace' : activeView === 'factory' ? 'Execution' : activeView === 'backlog' ? 'Plan ahead' : activeView === 'channels' ? 'Collaborate' : 'Human attention'}
        title={activeView === 'context' ? 'Project context' : activeView === 'overview' ? 'Work overview' : activeView === 'factory' ? 'Task board' : activeView === 'backlog' ? 'Backlog' : activeView === 'channels' ? 'Channels' : 'Decision inbox'}
        description={activeView === 'context' ? 'Connect project sources once and reuse their exact revisions across tasks.' : activeView === 'overview' ? 'A clear view of what needs you, what’s moving, and what’s ready to review.' : activeView === 'factory' ? 'Follow every task from authorization to a verified result.' : activeView === 'backlog' ? 'Define the outcome and its boundaries before work begins.' : activeView === 'channels' ? 'Discuss the work, share context, and turn an idea into a task.' : 'Review the context and choose how work should proceed.'}
      />

      {#if activeView === 'overview'}
        <div class="workspace-summary" aria-label="Work summary">
          <div><span class="summary-icon attention"><Inbox size={19} /></span><span><small>Needs attention</small><strong>{queues.attention.length}</strong></span><span class="summary-note">Your input matters</span></div>
          <div><span class="summary-icon"><CircleGauge size={19} /></span><span><small>In progress</small><strong>{queues.active.length}</strong></span><span class="summary-note">Moving forward</span></div>
          <div><span class="summary-icon results"><CheckCircle2 size={19} /></span><span><small>Results</small><strong>{queues.results.length}</strong></span><span class="summary-note">Ready to inspect</span></div>
        </div>
      {/if}

      {#if error}
        <div class="error-banner" role="alert">
          <span>{error}</span>
          <button type="button" on:click={() => (error = '')} aria-label="Dismiss error"><X size={14} /></button>
        </div>
      {/if}

      {#if loading}
        <div class="loading-layout">
          <div class="skeleton loading-strip"></div>
          <div class="loading-columns">
            {#each Array(3) as _}<div class="skeleton"></div>{/each}
          </div>
        </div>
      {:else if activeView === 'overview'}
        <WorkOverview searching={Boolean(normalizedSearch)} tasks={filteredTasks} {agents} onselect={openTask} />
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
          <section class="backlog-panel surface">
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
          <aside class="backlog-guide surface">
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
      {:else if activeView === 'context'}
        <ProjectContext />
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
  .auth-shell { display: grid; width: 100%; min-height: 100dvh; padding: 24px; background: radial-gradient(ellipse at 80% 0, var(--accent-soft), transparent 65%), var(--bg); place-items: center; }
  .auth-card { width: min(440px, 100%); padding: 36px; background: var(--panel); border: 1px solid var(--line); border-radius: var(--radius-lg); box-shadow: var(--shadow); }
  .auth-brand { margin-bottom: 40px; color: var(--accent); }
  .auth-card > .eyebrow { color: var(--accent); }
  .auth-card h1 { margin: 10px 0; font-size: 1.75rem; letter-spacing: -.035em; line-height: 1.2; }
  .auth-card > p { color: var(--muted); }
  .auth-card form { display: grid; gap: 10px; margin-top: 28px; }
  .auth-card label { font-size: var(--text-sm); font-weight: 600; }
  .auth-card input { width: 100%; height: 44px; padding: 0 12px; color: var(--text); background: var(--panel); border: 1px solid var(--line-strong); border-radius: var(--radius-sm); }
  .auth-card form button { display: flex; height: 44px; align-items: center; justify-content: center; gap: 8px; margin-top: 6px; color: var(--on-accent); font-weight: 600; background: var(--accent); border: 0; border-radius: var(--radius-sm); cursor: pointer; }
  .auth-card form button:disabled { opacity: .55; }
  .auth-error { color: var(--danger); font-size: var(--text-sm); }
  .auth-note { display: block; margin-top: 20px; color: var(--subtle); font-size: var(--text-xs); }
  .auth-loading { display: flex; align-items: center; gap: 10px; color: var(--muted); }
  .auth-loading span:first-child { width: 9px; height: 9px; background: var(--accent); border-radius: 50%; }

  .app-shell { display: flex; width: 100%; height: 100vh; height: 100dvh; overflow: hidden; }
  .sidebar {
    /* A dark navigation surface with its own accessible semantic colors. */
    --text: #f0f6f8; --muted: #bfd0d8; --subtle: #9eb7c3; --accent: #89d3d4;
    --accent-soft: #214a58; --warning: #f3cf8b; --warning-soft: #3c3c32;
    --line: #2c4653; --line-strong: #52717e;
    z-index: 30; display: flex; width: 232px; flex: 0 0 auto; flex-direction: column; padding: 28px 16px 20px;
    color: var(--text); background: var(--sidebar); border-right: 1px solid var(--line); overflow-y: auto;
  }
  .brand { display: flex; align-items: center; gap: 11px; }
  .sidebar .brand { padding: 0 10px; color: var(--accent); }
  .brand strong, .brand small { display: block; }
  .brand strong { color: var(--text); font-size: 1.3rem; font-weight: 650; letter-spacing: -.03em; line-height: 1.2; }
  .brand small { margin-top: 5px; color: var(--subtle); font-size: .625rem; font-weight: 600; letter-spacing: .15em; }
  .workspace-identity { display: flex; align-items: center; gap: 10px; margin-top: 32px; padding: 14px 10px; border-top: 1px solid var(--line); border-bottom: 1px solid var(--line); }
  .workspace-icon { display: grid; width: 32px; height: 32px; color: var(--accent); background: var(--accent-soft); border-radius: 8px; place-items: center; }
  .workspace-identity small, .workspace-identity strong { display: block; }
  .workspace-identity small { color: var(--subtle); font-size: var(--text-xs); }
  .workspace-identity strong { font-size: var(--text-sm); font-weight: 600; }
  nav { display: grid; gap: 4px; margin-top: 28px; }
  .nav-label { margin: 0 12px 8px; color: var(--subtle); font-size: .6875rem; font-weight: 600; letter-spacing: .1em; text-transform: uppercase; }
  .secondary-label { margin-top: 24px; }
  nav button { position: relative; display: flex; width: 100%; min-height: 44px; align-items: center; gap: 11px; padding: 0 12px; color: var(--muted); font-size: var(--text-sm); font-weight: 500; text-align: left; background: transparent; border: 1px solid transparent; border-radius: var(--radius-sm); cursor: pointer; }
  nav button:hover { background: #1b3b4a; color: var(--text); }
  nav button.active { color: var(--text); background: #234856; border-color: #315663; }
  nav button.active > :global(svg) { color: var(--accent); }
  nav button span { flex: 1; }
  nav button em { min-width: 22px; padding: 1px 5px; color: var(--muted); font-size: var(--text-xs); font-style: normal; text-align: center; background: #193a49; border-radius: 4px; }
  nav button em.attention-count { color: var(--warning); background: var(--warning-soft); }
  .sidebar-bottom { margin-top: auto; padding-top: 40px; }
  .system-card { padding: 12px; background: #173746; border: 1px solid var(--line); border-radius: var(--radius-md); }
  .system-top { display: flex; align-items: center; justify-content: space-between; gap: 8px; color: var(--muted); font-size: var(--text-xs); }
  .system-top span { display: flex; align-items: center; gap: 6px; }
  .system-top i { width: 6px; height: 6px; flex-shrink: 0; background: var(--accent); border-radius: 50%; }
  .system-top i.offline { background: var(--warning); }
  .system-stats { display: flex; gap: 20px; margin-top: 12px; color: var(--subtle); font-size: var(--text-xs); }
  .system-stats strong { display: block; margin-bottom: 2px; color: var(--text); font-weight: 500; }
  .system-warning { display: block; margin-top: 10px; color: var(--warning); overflow-wrap: anywhere; }
  .platform-signature { display: flex; align-items: center; gap: 10px; margin: 22px 10px 0; color: var(--subtle); }
  .platform-signature strong, .platform-signature small { display: block; font-size: var(--text-xs); }
  .platform-signature strong { color: var(--muted); font-weight: 500; }
  .platform-signature small { margin-top: 2px; font-size: .6875rem; }

  main { display: flex; min-width: 0; flex: 1; flex-direction: column; }
  .topbar { display: flex; min-height: 72px; flex: 0 0 auto; align-items: center; justify-content: space-between; gap: 16px; padding: 0 32px; background: var(--panel); border-bottom: 1px solid var(--line); }
  .location { display: flex; align-items: center; gap: 12px; color: var(--subtle); font-size: var(--text-sm); text-transform: capitalize; white-space: nowrap; }
  .location strong { color: var(--text); font-weight: 500; }
  .topbar-actions { display: flex; align-items: center; gap: 10px; }
  .search-box { display: flex; width: 200px; height: 38px; align-items: center; gap: 8px; padding: 0 10px; color: var(--subtle); background: var(--bg); border: 1px solid var(--line); border-radius: var(--radius-sm); }
  .search-box input { width: 100%; min-width: 0; color: var(--text); font-size: var(--text-sm); background: transparent; border: 0; }
  .search-box button { display: grid; width: 22px; height: 28px; color: var(--subtle); background: transparent; border: 0; cursor: pointer; place-items: center; }
  .connection { display: flex; align-items: center; gap: 6px; color: var(--accent); font-size: var(--text-xs); white-space: nowrap; }
  .connection.offline { color: var(--warning); }
  .export-link, .sign-out { display: grid; width: 36px; height: 38px; flex-shrink: 0; color: var(--muted); background: transparent; border: 1px solid var(--line); border-radius: var(--radius-sm); cursor: pointer; place-items: center; }
  .export-link:hover, .sign-out:hover { background: var(--panel-soft); }
  .import-link { position: relative; }
  .import-link input { position: absolute; width: 1px; height: 1px; opacity: 0; }
  .operator-name { max-width: 110px; overflow: hidden; font-size: var(--text-xs); text-overflow: ellipsis; white-space: nowrap; color: var(--muted); }
  .new-task, .list-header button { display: inline-flex; height: 38px; flex-shrink: 0; align-items: center; justify-content: center; gap: 7px; padding: 0 14px; color: var(--on-accent); font-size: var(--text-sm); font-weight: 600; background: var(--accent); border: 1px solid transparent; border-radius: var(--radius-sm); cursor: pointer; }
  .new-task:hover { background: var(--accent-hover); }
  .menu-button { display: none; }
  .workspace { display: flex; min-height: 0; flex: 1; flex-direction: column; padding: 32px; overflow-y: auto; }
  .workspace-summary { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 16px; margin-bottom: 28px; flex-shrink: 0; }
  .workspace-summary > div { display: flex; align-items: center; gap: 14px; padding: 18px; background: var(--panel); border: 1px solid var(--line); border-radius: var(--radius-md); box-shadow: var(--shadow-card); }
  .summary-icon { display: grid; width: 42px; height: 42px; flex-shrink: 0; color: var(--accent); background: var(--accent-soft); border-radius: var(--radius-md); place-items: center; }
  .summary-icon.attention { color: var(--warning); background: var(--warning-soft); }
  .summary-icon.results { color: var(--success); background: var(--success-soft); }
  .workspace-summary small { display: block; color: var(--muted); font-size: var(--text-xs); }
  .workspace-summary strong { display: block; font-size: 1.75rem; font-weight: 600; line-height: 1.25; font-variant-numeric: tabular-nums; }
  .summary-note { margin-left: auto; color: var(--subtle); font-size: var(--text-xs); }
  .error-banner { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 16px; padding: 12px; color: var(--danger); background: var(--danger-soft); border-radius: var(--radius-sm); }
  .error-banner button { display: grid; width: 28px; height: 28px; color: inherit; background: transparent; border: 0; cursor: pointer; place-items: center; }
  .factory-stack { display: flex; min-height: 540px; flex: 1; flex-direction: column; gap: 20px; }
  .factory-body { display: flex; min-height: 0; flex: 1; gap: 20px; }
  .board { display: flex; min-width: 0; flex: 1; gap: 16px; padding: 1px 1px 10px; overflow-x: auto; scroll-snap-type: x proximity; }
  .board-column { display: flex; width: 280px; min-width: 280px; height: 100%; flex-direction: column; background: var(--panel-soft); border: 1px solid var(--line); border-radius: var(--radius-md); scroll-snap-align: start; }
  .board-column.drop-target { background: var(--accent-soft); border-color: var(--accent); }
  .board-column > header { flex: 0 0 auto; padding: 16px; border-bottom: 1px solid var(--line); }
  .board-column > header > div { display: flex; align-items: center; gap: 8px; }
  .board-column > header strong { font-size: var(--text-sm); font-weight: 600; }
  .board-column > header em { margin-left: auto; font-size: var(--text-xs); font-style: normal; color: var(--muted); }
  .board-column > header small { display: block; margin: 5px 0 0 15px; color: var(--subtle); font-size: var(--text-xs); }
  .column-signal { width: 7px; height: 7px; background: var(--info); border-radius: 50%; }
  .column-planning .column-signal { background: var(--planning); }
  .column-waiting_decision .column-signal { background: var(--warning); }
  .column-running .column-signal { background: var(--accent); }
  .column-done .column-signal { background: var(--success); }
  .column-blocked .column-signal { background: var(--danger); }
  .column-cards { display: flex; min-height: 0; flex: 1; flex-direction: column; gap: 12px; padding: 12px; overflow-y: auto; }
  .empty-column { display: grid; min-height: 140px; flex: 0 0 auto; place-content: center; justify-items: center; gap: 10px; color: var(--subtle); font-size: var(--text-xs); border: 1px dashed var(--line-strong); border-radius: var(--radius-sm); }
  .loading-layout { display: flex; min-height: 350px; flex: 1; flex-direction: column; gap: 16px; }
  .loading-strip { height: 86px; border-radius: var(--radius-md); }
  .loading-columns { display: grid; flex: 1; grid-template-columns: repeat(3, 1fr); gap: 16px; }
  .loading-columns > div { border-radius: var(--radius-md); }
  .backlog-layout { display: grid; min-height: 400px; flex: 1; grid-template-columns: minmax(0, 1fr) 260px; gap: 20px; }
  .backlog-panel { min-width: 0; min-height: 0; overflow: hidden; border-radius: var(--radius-md); }
  .list-header { display: flex; min-height: 88px; align-items: center; justify-content: space-between; gap: 12px; padding: 20px; border-bottom: 1px solid var(--line); }
  .list-header h2 { margin: 4px 0 0; font-size: var(--text-lg); font-weight: 600; }
  .list-header button { color: var(--accent); background: var(--panel); border-color: var(--line-strong); }
  .backlog-list { height: calc(100% - 88px); overflow-y: auto; }
  .backlog-list article { display: grid; min-height: 100px; grid-template-columns: 68px minmax(0, 1fr) 42px 86px 72px; align-items: center; gap: 12px; padding: 16px 20px; border-bottom: 1px solid var(--line); }
  .backlog-list article:hover { background: var(--surface-hover); }
  .backlog-priority { display: flex; align-items: center; gap: 6px; color: var(--info); font-size: .6875rem; font-weight: 600; text-transform: uppercase; }
  .backlog-priority i { width: 5px; height: 5px; background: currentColor; border-radius: 50%; }
  .backlog-priority.priority-urgent { color: var(--danger); }
  .backlog-priority.priority-high { color: var(--planning); }
  .backlog-priority.priority-low { color: var(--subtle); }
  .backlog-copy { min-width: 0; padding: 0; text-align: left; background: transparent; border: 0; cursor: pointer; }
  .backlog-copy strong, .backlog-copy span { display: block; }
  .backlog-copy strong { font-size: var(--text-base); font-weight: 600; }
  .backlog-copy span { margin-top: 5px; color: var(--muted); font-size: var(--text-sm); }
  .backlog-budget, .backlog-date { color: var(--subtle); font-size: var(--text-xs); }
  .promote { display: inline-flex; min-height: 36px; align-items: center; justify-content: center; gap: 4px; color: var(--accent); font-size: var(--text-xs); font-weight: 600; background: var(--accent-soft); border: 0; border-radius: var(--radius-sm); cursor: pointer; }
  .backlog-empty { display: grid; min-height: 300px; padding: 24px; place-content: center; justify-items: center; color: var(--subtle); text-align: center; }
  .backlog-empty h3 { margin: 12px 0 4px; color: var(--text); font-size: var(--text-base); }
  .backlog-empty p { margin: 0; font-size: var(--text-sm); }
  .backlog-guide { height: fit-content; padding: 20px; border-radius: var(--radius-md); }
  .guide-icon { display: grid; width: 40px; height: 40px; margin-bottom: 18px; color: var(--accent); background: var(--accent-soft); border-radius: var(--radius-md); place-items: center; }
  .backlog-guide h3 { margin: 8px 0 20px; font-size: var(--text-lg); font-weight: 600; }
  .backlog-guide ul { display: grid; gap: 14px; margin: 0; padding: 0; list-style: none; }
  .backlog-guide li { display: flex; align-items: center; gap: 8px; color: var(--muted); font-size: var(--text-sm); }
  .backlog-guide li :global(svg) { color: var(--accent); }
  .detail-loading { position: fixed; z-index: 90; top: 88px; right: 24px; display: grid; width: 36px; height: 36px; background: var(--panel); border: 1px solid var(--line); border-radius: var(--radius-sm); box-shadow: var(--shadow); place-items: center; }
  .detail-loading span { width: 16px; height: 16px; border: 2px solid var(--line); border-top-color: var(--accent); border-radius: 50%; animation: spin 700ms linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
  .mobile-scrim { display: none; }
  @media (max-width: 1400px) { .summary-note { display: none; } .backlog-layout { grid-template-columns: minmax(0, 1fr); } .backlog-guide { display: none; } }
  @media (max-width: 1200px) { .connection { display: none; } .operator-name { display: none; } .workspace { padding: 24px; } .topbar { padding: 0 24px; } .search-box { width: 180px; } }
  @media (max-width: 980px) {
    .sidebar { position: fixed; top: 0; bottom: 0; left: 0; transform: translateX(-100%); visibility: hidden; transition: transform 180ms ease; }
    .sidebar.mobile-open { transform: translateX(0); visibility: visible; }
    .mobile-scrim { position: fixed; z-index: 25; inset: 0; display: block; background: var(--scrim); border: 0; }
    .menu-button { display: grid; width: 38px; height: 38px; flex-shrink: 0; color: var(--muted); background: transparent; border: 1px solid var(--line); border-radius: var(--radius-sm); place-items: center; }
    .location { margin-right: auto; }
  }
  @media (max-width: 760px) {
    .workspace { padding: 24px 16px; }
    .topbar { gap: 10px; padding: 0 16px; min-height: 64px; }
    .location { display: none; }
    .topbar-actions { min-width: 0; flex: 1; gap: 8px; }
    .search-box { width: auto; min-width: 60px; flex: 1; }
    .workspace-summary { gap: 8px; margin-bottom: 24px; }
    .workspace-summary > div { align-items: flex-start; flex-direction: column; padding: 12px; gap: 10px; }
    .summary-icon { width: 32px; height: 32px; }
    .workspace-summary strong { font-size: 1.5rem; }
    .backlog-list article { grid-template-columns: minmax(0, 1fr) 68px; }
    .backlog-date, .backlog-budget, .backlog-priority { display: none; }
    .list-header { padding: 16px; }
    .list-header h2 { font-size: var(--text-base); }
  }
  @media (max-width: 520px) {
    .new-task { width: 38px; padding: 0; font-size: 0; }
    .export-link, .sign-out { width: 30px; }
    .auth-card { padding: 24px; }
  }
</style>
