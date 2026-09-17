<script lang="ts">
  import { modal } from '$lib/dialog';
  import {
    ArrowRight,
    Bot,
    Check,
    CheckCircle2,
    ChevronRight,
    Clock3,
    FileCheck2,
    GitPullRequestArrow,
    History,
    ListChecks,
    Pencil,
    RotateCcw,
    Save,
    ShieldCheck,
    X
  } from '@lucide/svelte';
  import { eventLabel, formatDate, initials, timeAgo } from '$lib/format';
  import { api } from '$lib/api';
  import type {
    Agent,
    Decision,
    Priority,
    Task,
    TaskDetail,
    TaskStatus,
    UpdateTaskInput
  } from '$lib/types';

  export let detail: TaskDetail;
  export let agents: Agent[] = [];
  export let onclose: () => void;
  export let onrefresh: () => Promise<void>;
  export let onmove: (task: Task, status: TaskStatus) => Promise<void>;
  export let onsave: (task: Task, input: UpdateTaskInput) => Promise<void>;
  export let onanswer: (decision: Decision, answer: string) => Promise<void>;

  let tab: 'overview' | 'activity' | 'evidence' = 'overview';
  let editing = false;
  let busy = false;
  let error = '';
  let loadedID = '';
  let title = '';
  let description = '';
  let acceptanceCriteria = '';
  let priority: Priority = 'medium';
  let budgetMinutes = 30;
  let maxAttempts = 1;
  let maxTokens = 0;
  let maxCostMinor = 0;
  let currency = 'USD';
  let maxConcurrency = 1;
  let authority = '';
  let deadline = '';

  async function cancelRun() {
    if (!run) return;
    busy = true; error = '';
    try { await api.cancelRun(run.id); await onrefresh(); }
    catch (caught) { error = caught instanceof Error ? caught.message : 'Cancellation failed'; }
    finally { busy = false; }
  }

  $: task = detail.task;
  $: run = detail.runs[0];
  $: resultURL = run?.result_summary.match(/https:\/\/[^\s)]+/)?.[0];
  $: usageEvent = detail.events.find((event) => typeof event.payload?.reported_tokens === 'number');
  $: agent = agents.find((candidate) => candidate.id === (run?.agent_id ?? task.assigned_agent_id));
  $: pendingDecision = detail.decisions.find((decision) => decision.status === 'pending');
  $: progressEvent = detail.events.find(
    (event) => event.type === 'run.progress' && typeof event.payload?.percent === 'number'
  );
  $: progress = progressEvent ? Number(progressEvent.payload?.percent) : task.status === 'done' ? 100 : 0;
  $: if (task.id !== loadedID) {
    loadedID = task.id;
    title = task.title;
    description = task.description;
    acceptanceCriteria = task.acceptance_criteria;
    priority = task.priority;
    budgetMinutes = task.budget_minutes;
    maxAttempts = task.budget.max_attempts;
    maxTokens = task.budget.max_tokens;
    maxCostMinor = task.budget.max_cost_minor;
    currency = task.budget.currency;
    maxConcurrency = task.budget.max_concurrency;
    authority = task.budget.authority.join(', ');
    deadline = task.budget.deadline_at ? new Date(task.budget.deadline_at).toISOString().slice(0, 16) : '';
    editing = false;
    tab = 'overview';
    error = '';
  }

  function targetStatus(current: TaskStatus): TaskStatus | null {
    if (current === 'backlog') return 'ready';
    if (current === 'ready') return 'backlog';
    if (['blocked', 'failed', 'cancelled', 'done'].includes(current)) return 'ready';
    return null;
  }

  function actionLabel(current: TaskStatus): string {
    if (current === 'backlog') return 'Move to ready';
    if (current === 'ready') return 'Return to backlog';
    if (current === 'done') return 'Run again';
    return 'Requeue task';
  }

  async function move() {
    const target = targetStatus(task.status);
    if (!target) return;
    busy = true;
    error = '';
    try {
      await onmove(task, target);
      await onrefresh();
    } catch (caught) {
      error = caught instanceof Error ? caught.message : 'Could not move the task.';
    } finally {
      busy = false;
    }
  }

  async function save() {
    if (!title.trim()) {
      error = 'The outcome cannot be empty.';
      return;
    }
    busy = true;
    error = '';
    try {
      await onsave(task, {
        title: title.trim(),
        description: description.trim(),
        acceptance_criteria: acceptanceCriteria.trim(),
        priority,
        budget_minutes: Number(budgetMinutes),
        budget: {
          time_minutes: Number(budgetMinutes),
          ...(deadline ? { deadline_at: new Date(deadline).getTime() } : {}),
          max_attempts: Number(maxAttempts),
          max_tokens: Number(maxTokens),
          max_cost_minor: Number(maxCostMinor),
          currency: currency.trim().toUpperCase(),
          max_concurrency: Number(maxConcurrency),
          authority: authority.split(',').map((item) => item.trim()).filter(Boolean)
        },
        version: task.version
      });
      editing = false;
      await onrefresh();
    } catch (caught) {
      error = caught instanceof Error ? caught.message : 'Could not save the task.';
    } finally {
      busy = false;
    }
  }

  async function answer(decision: Decision, option: string) {
    busy = true;
    error = '';
    try {
      await onanswer(decision, option);
      await onrefresh();
    } catch (caught) {
      error = caught instanceof Error ? caught.message : 'Could not record the decision.';
    } finally {
      busy = false;
    }
  }


  function displayLabel(value: string): string {
    return value.split('-').map((part) => part.charAt(0).toUpperCase() + part.slice(1)).join(' ');
  }
</script>

<dialog class="drawer-backdrop" use:modal aria-labelledby="task-title" on:cancel|preventDefault={onclose} on:click|self={onclose}>
  <div class="drawer">
    <header class="drawer-header">
      <div class="breadcrumb">
        <span>DECK</span><ChevronRight size={12} /><span>{task.id.slice(-6).toUpperCase()}</span>
      </div>
      <div class="header-actions">
        {#if !editing}
          <button type="button" on:click={() => (editing = true)} aria-label="Edit task">
            <Pencil size={15} />
          </button>
        {/if}
        <button type="button" on:click={onclose} data-dialog-focus aria-label="Close task">
          <X size={17} />
        </button>
      </div>
    </header>

    <div class="drawer-scroll">
      <section class="task-heading">
        <div class="status-line">
          <span class="status status-{task.status}"><i></i>{displayLabel(detail.display_state)}</span>
          <span class="priority priority-{task.priority}">{task.priority} priority</span>
        </div>

        {#if editing}
          <input class="title-input" bind:value={title} aria-label="Task title" />
        {:else}
          <h1 id="task-title">{task.title}</h1>
        {/if}

        <div class="task-meta">
          <span><Clock3 size={13} /> {task.budget_minutes} minute budget</span>
          <span><History size={13} /> Updated {timeAgo(task.updated_at)}</span>
        </div>
        <div class="budget-summary">
          <span>{task.budget.max_attempts} attempt{task.budget.max_attempts === 1 ? '' : 's'}</span>
          <span>{task.budget.max_tokens ? `${task.budget.max_tokens.toLocaleString()} tokens` : 'No token cap'}</span>
          <span>{task.budget.max_cost_minor ? `${(task.budget.max_cost_minor / 100).toFixed(2)} ${task.budget.currency}` : 'No cost cap'}</span>
          <span>Concurrency {task.budget.max_concurrency}</span>
        </div>
      </section>

      <section class="provider-truth state-{detail.provider_status.connectivity}">
        <div><span>Provider</span><strong>{detail.provider_status.connectivity}</strong></div>
        <div><span>Synchronization</span><strong>{detail.provider_status.sync_state}</strong><small>{Math.round(detail.provider_status.sync_lag_millis / 1000)}s lag</small></div>
        <div><span>Lease</span><strong>{detail.provider_status.lease_state}</strong>{#if detail.provider_status.lease_expires_at}<small>{formatDate(detail.provider_status.lease_expires_at)}</small>{/if}</div>
        <div><span>Projections</span><strong>{detail.provider_status.rejected_projection_count} rejected</strong><small>{detail.provider_status.stale_projection_count} stale</small></div>
        {#if detail.provider_status.degraded_dependencies.length}
          <p>Degraded: {detail.provider_status.degraded_dependencies.join(', ')}</p>
        {/if}
      </section>

      {#if run && ['running', 'verifying', 'done'].includes(task.status) && (progressEvent || task.status === 'done')}
        <section class="run-progress">
          <div class="progress-copy">
            <span>{task.status === 'done' ? 'Completed' : task.status === 'verifying' ? 'Verifying' : 'Executing approved plan'}</span>
            <strong>{progress}%</strong>
          </div>
          <div class="progress-track"><span style={`width: ${progress}%`}></span></div>
        </section>
      {/if}

      {#if pendingDecision}
        <section class="decision-callout">
          <div class="callout-title">
            <span class="callout-icon"><GitPullRequestArrow size={17} /></span>
            <div>
              <span class="eyebrow">Agent is paused</span>
              <h2>{pendingDecision.question}</h2>
            </div>
          </div>
          <div class="recommendation">
            Recommended: <strong>{pendingDecision.recommendation}</strong>
          </div>
          <div class="decision-facts">
            <span>Risk <strong>{pendingDecision.risk_class}</strong></span>
            <span>Cost <strong>{(pendingDecision.estimated_cost_minor / 100).toFixed(2)} {pendingDecision.currency}</strong></span>
            {#if pendingDecision.expires_at}<span>Expires <strong>{formatDate(pendingDecision.expires_at)}</strong></span>{/if}
          </div>
          {#if pendingDecision.evidence_summary}<p class="decision-context"><strong>Evidence:</strong> {pendingDecision.evidence_summary}</p>{/if}
          <p class="decision-context"><strong>If waiting:</strong> {pendingDecision.waiting_consequence}</p>
          <p class="decision-context"><strong>If rejected:</strong> {pendingDecision.rejecting_consequence}</p>
          <div class="decision-actions">
            {#each pendingDecision.options as option}
              <button
                type="button"
                class:approve={option === pendingDecision.recommendation}
                disabled={busy}
                on:click={() => answer(pendingDecision, option)}
              >
                {#if option === pendingDecision.recommendation}<Check size={15} />{/if}
                {option}
              </button>
            {/each}
          </div>
        </section>
      {/if}

      {#if error}<p class="drawer-error">{error}</p>{/if}
      {#if run && ['planning','waiting_decision','running','verifying'].includes(run.status)}
        <button class="cancel-run" type="button" disabled={busy || detail.commands.some((command) => command.type === 'cancel' && ['pending','acknowledged'].includes(command.status))} on:click={cancelRun}>Cancel run</button>
      {/if}
      {#each detail.commands as command (command.id)}
        <p class="command-status">{command.type}: {command.status}{command.result ? ` · ${command.result}` : ''}</p>
      {/each}
      {#if usageEvent?.payload}
        <p class="command-status">Reported model usage: {Number(usageEvent.payload.reported_tokens).toLocaleString()} tokens · {(Number(usageEvent.payload.reported_cost_micros) / 1000000).toFixed(4)} {String(usageEvent.payload.currency ?? '')}</p>
      {/if}

      <nav class="tabs" aria-label="Task detail sections">
        <button class:active={tab === 'overview'} type="button" on:click={() => (tab = 'overview')}>
          <ListChecks size={14} /> Overview
        </button>
        <button class:active={tab === 'activity'} type="button" on:click={() => (tab = 'activity')}>
          <History size={14} /> Activity <span>{detail.events.length}</span>
        </button>
        <button class:active={tab === 'evidence'} type="button" on:click={() => (tab = 'evidence')}>
          <FileCheck2 size={14} /> Evidence <span>{detail.evidence.length}</span>
        </button>
      </nav>

      {#if tab === 'overview'}
        <div class="tab-content overview">
          <section>
            <span class="section-label">Context</span>
            {#if editing}
              <textarea bind:value={description} rows="5" placeholder="Task context and constraints"></textarea>
            {:else}
              <p class:empty-copy={!task.description}>{task.description || 'No additional context yet.'}</p>
            {/if}
          </section>

          <section>
            <span class="section-label">Acceptance criteria</span>
            {#if editing}
              <textarea
                bind:value={acceptanceCriteria}
                rows="5"
                placeholder="Observable completion checks"
              ></textarea>
            {:else}
              <div class="acceptance">
                <span><ShieldCheck size={16} /></span>
                <p class:empty-copy={!task.acceptance_criteria}>
                  {task.acceptance_criteria || 'No acceptance criteria defined.'}
                </p>
              </div>
            {/if}
          </section>

          {#if editing}
            <section class="edit-grid">
              <label>
                Priority
                <select bind:value={priority}>
                  <option value="low">Low</option>
                  <option value="medium">Medium</option>
                  <option value="high">High</option>
                  <option value="urgent">Urgent</option>
                </select>
              </label>
              <label>
                Budget in minutes
                <input type="number" min="1" max="1440" bind:value={budgetMinutes} />
              </label>
            </section>
            <section class="edit-grid structured-budget">
              <label>Deadline<input type="datetime-local" bind:value={deadline} /></label>
              <label>Attempts<input type="number" min="1" bind:value={maxAttempts} /></label>
              <label>Tokens<input type="number" min="0" bind:value={maxTokens} /></label>
              <label>Cost, minor units<input type="number" min="0" bind:value={maxCostMinor} /></label>
              <label>Currency<input maxlength="3" bind:value={currency} /></label>
              <label>Concurrency<input type="number" min="1" bind:value={maxConcurrency} /></label>
              <label class="wide">Authority<input bind:value={authority} placeholder="source.read, pull_request.create" /></label>
            </section>
            <div class="edit-actions">
              <button class="secondary" type="button" on:click={() => (editing = false)}>Cancel</button>
              <button class="primary" type="button" disabled={busy} on:click={save}>
                <Save size={15} /> Save changes
              </button>
            </div>
          {/if}

          {#if run}
            <section>
              <span class="section-label">Latest run</span>
              <div class="run-card">
                <div class="run-agent">
                  <span class="agent-avatar">
                    {#if agent}{initials(agent.name)}{:else}<Bot size={14} />{/if}
                  </span>
                  <div>
                    <strong>{agent?.name ?? run.agent_id}</strong>
                    <small>{run.status} · started {formatDate(run.started_at)}</small>
                  </div>
                </div>
                {#if run.plan}
                  <div class="plan">
                    <span>Approved plan</span>
                    <pre>{run.plan}</pre>
                  </div>
                {/if}
                {#if run.result_summary}
                  <div class="result">
                    <CheckCircle2 size={15} />
                    {run.result_summary}
                  </div>
                  {#if resultURL}<a href={resultURL} target="_blank" rel="noopener noreferrer">Review pull request</a>{/if}
                {/if}
              </div>
            </section>
          {/if}

          {#if detail.plan_revisions.length}
            <section>
              <span class="section-label">Immutable plan history</span>
              <div class="plan-history">
                {#each detail.plan_revisions as plan (plan.id)}
                  <article>
                    <header><strong>Revision {plan.revision}</strong><span>{plan.risk_class} risk · {formatDate(plan.created_at)}</span></header>
                    <p>{plan.plan}</p>
                    <dl>
                      <div><dt>Context</dt><dd>{plan.context_digest}</dd></div>
                      <div><dt>Verification</dt><dd>{plan.verification_strategy}</dd></div>
                      <div><dt>Side effects</dt><dd>{plan.expected_side_effects.join(', ') || 'None reported'}</dd></div>
                      <div><dt>Rollback</dt><dd>{plan.rollback_approach}</dd></div>
                    </dl>
                  </article>
                {/each}
              </div>
            </section>
          {/if}

          {#if detail.operational_states.length}
            <section>
              <span class="section-label">Delivery and observation</span>
              <div class="operations-grid">
                {#each detail.operational_states as state (state.id)}
                  <article>
                    <strong>{state.environment}</strong><span>Rollout {state.rollout_state}</span>
                    <span>Rollback {state.rollback_state}</span><span>Policy {state.policy_version}</span>
                    {#if state.observation_started_at}<small>Observed {formatDate(state.observation_started_at)}{state.observation_ends_at ? ` – ${formatDate(state.observation_ends_at)}` : ''}</small>{/if}
                  </article>
                {/each}
              </div>
            </section>
          {/if}
        </div>
      {:else if tab === 'activity'}
        <div class="tab-content timeline">
          {#each detail.trail as event (event.id)}
            <article class:decision-event={event.stage === 'decision'}>
              <span class="timeline-dot"></span>
              <div class="timeline-copy">
                <div>
                  <strong>{event.stage} · {eventLabel(event.type)}</strong>
                  <time>{formatDate(event.occurred_at)}</time>
                </div>
                <p>{event.title}</p>
              </div>
            </article>
          {/each}
        </div>
      {:else}
        <div class="tab-content evidence-list">
          {#each detail.evidence as evidence (evidence.id)}
            <article class="evidence-record status-{evidence.admission_status}">
              <header><div><strong>{evidence.type} · {evidence.outcome}</strong><small>{evidence.admission_status} · observed {formatDate(evidence.observed_at)}</small></div></header>
              <dl>
                <div><dt>Producer</dt><dd>{evidence.producer}</dd></div>
                <div><dt>Verifier</dt><dd>{evidence.verifier}</dd></div>
                <div><dt>Command</dt><dd>{evidence.command}</dd></div>
                <div><dt>Subject</dt><dd>{evidence.subject_digest}</dd></div>
                <div><dt>Artifact</dt><dd>{evidence.artifact_digest}</dd></div>
                <div><dt>Source</dt><dd>{evidence.source_revision}</dd></div>
              </dl>
              <a href={`/api/v1/operator/tasks/${encodeURIComponent(task.id)}/artifacts/${encodeURIComponent(evidence.artifact_digest)}`}>Download verification result</a>
            </article>
          {/each}
          {#each detail.artifacts as artifact (artifact.id)}
            <article class="artifact">
              <header>
                <span><FileCheck2 size={16} /></span>
                <div>
                  <strong>{artifact.name}</strong>
                  <small>{artifact.kind} · {formatDate(artifact.created_at)}</small>
                </div>
              </header>
              <pre>{artifact.content}</pre>
            </article>
          {/each}
          {#if detail.artifacts.length === 0 && detail.evidence.length === 0}
            <div class="empty-evidence">
              <FileCheck2 size={25} />
              <h3>No evidence attached yet</h3>
              <p>Structured records and generated reports will appear after verification.</p>
            </div>
          {/if}
        </div>
      {/if}
    </div>

    {#if targetStatus(task.status) && !editing}
      <footer class="drawer-footer">
        <button type="button" disabled={busy} on:click={move}>
          {#if task.status === 'backlog'}<ArrowRight size={15} />{:else}<RotateCcw size={15} />{/if}
          {busy ? 'Moving…' : actionLabel(task.status)}
        </button>
      </footer>
    {/if}
  </div>
</dialog>

<style>
  .cancel-run { margin:12px 24px; padding:8px 12px; border:1px solid var(--line); border-radius:8px; color:var(--text); background:transparent; }
  .command-status { margin:8px 24px; color:var(--subtle); font-size: var(--text-sm); }
  .drawer-backdrop {
    width: 100%;
    max-width: none;
    height: 100dvh;
    max-height: none;
    margin: 0;
    border: 0;
    color: var(--text);
    position: fixed;
    z-index: 70;
    inset: 0;
    background: transparent;
    padding: 0;
  }

  .drawer-backdrop::backdrop { background: var(--scrim); backdrop-filter: blur(3px); }

  .drawer {
    position: absolute;
    top: 0;
    right: 0;
    display: flex;
    width: min(620px, 100%);
    height: 100%;
    flex-direction: column;
    background: var(--panel);
    border-left: 1px solid var(--line-strong);
    box-shadow: var(--shadow);
    animation: enter 180ms ease-out;
  }

  @keyframes enter {
    from { transform: translateX(28px); opacity: 0.4; }
  }

  .drawer-header,
  .header-actions,
  .breadcrumb,
  .status-line,
  .task-meta,
  .progress-copy,
  .callout-title,
  .decision-actions,
  .tabs,
  .acceptance,
  .run-agent,
  .result,
  .artifact header,
  .edit-actions {
    display: flex;
    align-items: center;
  }

  .drawer-header {
    height: 58px;
    flex: 0 0 auto;
    justify-content: space-between;
    padding: 0 18px 0 22px;
    border-bottom: 1px solid var(--line);
  }

  .breadcrumb {
    gap: 5px;
    color: var(--subtle);
    font-family: "SFMono-Regular", Consolas, monospace;
    font-size: var(--text-xs);
  }

  .header-actions {
    gap: 7px;
  }

  .header-actions button {
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

  .drawer-scroll {
    min-height: 0;
    flex: 1;
    overflow-y: auto;
  }

  .task-heading {
    padding: 24px 24px 18px;
  }

  .status-line {
    justify-content: space-between;
  }

  .status,
  .priority {
    font-size: var(--text-xs);
    font-weight: 750;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  .status {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    color: var(--info);
  }

  .status i {
    width: 7px;
    height: 7px;
    background: currentColor;
    border-radius: 999px;
    box-shadow: none;
    opacity: 0.9;
  }

  .status i::after { opacity: 0.1; }
  .status-waiting_decision { color: var(--warning); }
  .status-running { color: var(--accent); }
  .status-done { color: var(--success); }
  .status-blocked, .status-failed { color: var(--danger); }
  .status-backlog, .status-cancelled { color: var(--subtle); }
  .status-planning { color: var(--planning); }

  .priority { color: var(--muted); }
  .priority-urgent { color: var(--danger); }
  .priority-high { color: var(--planning); }

  h1 {
    margin: 17px 0 13px;
    font-size: clamp(1.35rem, 3vw, 1.7rem);
    font-weight: 650;
    letter-spacing: -0.035em;
    line-height: 1.22;
  }

  .title-input {
    width: 100%;
    margin: 15px 0 11px;
    padding: 8px 10px;
    color: var(--text);
    font-size: 1.3rem;
    font-weight: 650;
    background: var(--panel-soft);
    border: 1px solid var(--line-strong);
    border-radius: 10px;
  }

  .task-meta {
    gap: 16px;
    color: var(--subtle);
    font-size: var(--text-sm);
  }

  .task-meta span {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }

  .run-progress {
    margin: 0 24px 18px;
    padding: 12px 14px;
    background: var(--panel-soft);
    border: 1px solid var(--line);
    border-radius: 11px;
  }

  .progress-copy {
    justify-content: space-between;
    color: var(--muted);
    font-size: var(--text-sm);
  }

  .progress-copy strong { color: var(--accent); }

  .progress-track {
    height: 4px;
    margin-top: 9px;
    overflow: hidden;
    background: var(--panel-soft);
    border-radius: 999px;
  }

  .progress-track span {
    display: block;
    height: 100%;
    background: var(--accent);
    border-radius: inherit;
    transition: width 400ms ease;
  }

  .decision-callout {
    margin: 0 24px 18px;
    padding: 15px;
    background: linear-gradient(135deg, var(--warning-soft), color-mix(in srgb, var(--warning) 3.5%, transparent));
    border: 1px solid color-mix(in srgb, var(--warning) 24%, transparent);
    border-radius: 13px;
  }

  .callout-title { gap: 11px; }

  .callout-icon {
    display: grid;
    width: 36px;
    height: 36px;
    flex: 0 0 auto;
    color: var(--warning);
    background: color-mix(in srgb, var(--warning) 13%, transparent);
    border-radius: 10px;
    place-items: center;
  }

  .decision-callout h2 {
    margin: 4px 0 0;
    font-size: 0.81rem;
    font-weight: 650;
    line-height: 1.4;
  }

  .recommendation {
    margin-top: 12px;
    color: var(--muted);
    font-size: var(--text-sm);
  }

  .recommendation strong { color: var(--text); }

  .decision-actions { flex-wrap: wrap; gap: 8px; margin-top: 12px; }

  .decision-actions button,
  .edit-actions button,
  .drawer-footer button {
    display: inline-flex;
    min-height: 37px;
    align-items: center;
    justify-content: center;
    gap: 7px;
    padding: 0 13px;
    color: var(--muted);
    font-size: var(--text-sm);
    font-weight: 700;
    background: transparent;
    border: 1px solid var(--line-strong);
    border-radius: 9px;
    cursor: pointer;
  }

  .decision-actions button.approve,
  .edit-actions .primary,
  .drawer-footer button {
    color: var(--on-accent);
    background: var(--accent);
    border-color: var(--accent);
  }

  .drawer-error {
    margin: 0 24px 14px;
    padding: 9px 11px;
    color: var(--danger);
    font-size: var(--text-sm);
    background: var(--danger-soft);
    border-radius: 9px;
  }

  .tabs {
    overflow-x: auto;
    flex-shrink: 0;
    position: sticky;
    z-index: 2;
    top: 0;
    gap: 2px;
    padding: 0 20px;
    background: var(--panel);
    border-top: 1px solid var(--line);
    border-bottom: 1px solid var(--line);
    backdrop-filter: blur(10px);
  }

  .tabs button {
    white-space: nowrap;
    position: relative;
    display: inline-flex;
    height: 46px;
    align-items: center;
    gap: 6px;
    padding: 0 10px;
    color: var(--muted);
    font-size: var(--text-sm);
    font-weight: 650;
    background: transparent;
    border: 0;
    cursor: pointer;
  }

  .tabs button.active { color: var(--text); }

  .tabs button.active::after {
    position: absolute;
    right: 8px;
    bottom: -1px;
    left: 8px;
    height: 2px;
    background: var(--accent);
    content: "";
  }

  .tabs button span {
    padding: 2px 5px;
    color: var(--subtle);
    font-size: var(--text-xs);
    background: var(--panel-soft);
    border-radius: 5px;
  }

  .tab-content { padding: 22px 24px 40px; }

  .overview { display: grid; gap: 23px; }

  .overview .section-label,
  .edit-grid label {
    display: block;
    margin-bottom: 8px;
    color: var(--subtle);
    font-size: var(--text-xs);
    font-weight: 750;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .overview section > p,
  .acceptance p {
    margin: 0;
    color: var(--muted);
    font-size: var(--text-sm);
    line-height: 1.65;
    white-space: pre-wrap;
  }

  .empty-copy { color: var(--subtle) !important; font-style: italic; }

  textarea,
  select,
  .edit-grid input {
    width: 100%;
    color: var(--text);
    background: var(--panel-soft);
    border: 1px solid var(--line-strong);
    border-radius: 10px;
  }

  textarea { padding: 10px 11px; line-height: 1.5; resize: vertical; }
  select, .edit-grid input { height: 39px; padding: 0 10px; }

  .acceptance {
    align-items: flex-start;
    gap: 11px;
    padding: 12px;
    background: var(--info-soft);
    border: 1px solid color-mix(in srgb, var(--info) 14%, transparent);
    border-radius: 11px;
  }

  .acceptance > span { color: var(--info); }

  .edit-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
  .structured-budget { padding: 12px; background: var(--panel-soft); border: 1px solid var(--line); border-radius: 11px; }
  .structured-budget .wide { grid-column: 1 / -1; }
  .edit-actions { justify-content: flex-end; gap: 8px; margin-top: -10px; }

  .budget-summary { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 10px; }
  .budget-summary span { padding: 4px 7px; color: var(--subtle); font-size: var(--text-xs); background: var(--panel-soft); border-radius: 6px; }

  .provider-truth { display: grid; grid-template-columns: repeat(4, 1fr); gap: 1px; margin: 0 28px 16px; overflow: hidden; background: var(--line); border: 1px solid var(--line); border-radius: 11px; }
  .provider-truth > div { display: grid; gap: 3px; padding: 10px; background: var(--panel); }
  .provider-truth span, .provider-truth small { color: var(--subtle); font-size: var(--text-xs); text-transform: uppercase; }
  .provider-truth strong { font-size: var(--text-sm); text-transform: capitalize; }
  .provider-truth p { grid-column: 1 / -1; margin: 0; padding: 8px 10px; color: var(--warning); font-size: var(--text-xs); background: var(--warning-soft); }

  .decision-facts { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 10px; color: var(--subtle); font-size: var(--text-xs); }
  .decision-context { margin: 8px 0 0; color: var(--muted); font-size: var(--text-sm); line-height: 1.45; }

  .plan-history, .operations-grid { display: grid; gap: 9px; }
  .plan-history article, .operations-grid article, .evidence-record { padding: 11px; background: var(--panel-soft); border: 1px solid var(--line); border-radius: 11px; }
  .plan-history header { display: flex; justify-content: space-between; gap: 10px; }
  .plan-history header span { color: var(--subtle); font-size: var(--text-xs); }
  .plan-history p { margin: 8px 0; color: var(--muted); font-size: var(--text-sm); line-height: 1.5; }
  dl { display: grid; gap: 6px; margin: 0; }
  dl div { display: grid; grid-template-columns: 88px 1fr; gap: 8px; }
  dt { color: var(--subtle); font-size: var(--text-xs); text-transform: uppercase; }
  dd { min-width: 0; margin: 0; overflow-wrap: anywhere; color: var(--muted); font-size: var(--text-xs); }
  .operations-grid { grid-template-columns: repeat(2, 1fr); }
  .operations-grid article { display: grid; gap: 4px; }
  .operations-grid span, .operations-grid small { color: var(--muted); font-size: var(--text-xs); }

  .evidence-record { border-left: 3px solid var(--accent); }
  .evidence-record.status-stale, .evidence-record.status-rejected { border-left-color: var(--danger); }
  .evidence-record header { margin-bottom: 10px; }
  .evidence-record strong, .evidence-record small { display: block; }
  .evidence-record small { margin-top: 3px; color: var(--subtle); font-size: var(--text-xs); }

  .run-card {
    overflow: hidden;
    background: var(--panel-soft);
    border: 1px solid var(--line);
    border-radius: 12px;
  }

  .run-agent { gap: 10px; padding: 12px; }

  .agent-avatar {
    display: grid;
    width: 34px;
    height: 34px;
    color: var(--on-accent);
    font-size: var(--text-xs);
    font-weight: 850;
    background: var(--accent);
    border-radius: 10px;
    place-items: center;
  }

  .run-agent strong, .run-agent small { display: block; }
  .run-agent strong { font-size: var(--text-sm); }
  .run-agent small { margin-top: 3px; color: var(--subtle); font-size: var(--text-xs); }

  .plan { padding: 12px; border-top: 1px solid var(--line); }
  .plan > span { color: var(--subtle); font-size: var(--text-xs); font-weight: 750; text-transform: uppercase; }
  .plan pre, .artifact pre {
    overflow-wrap: anywhere;
    margin: 8px 0 0;
    color: var(--muted);
    font-family: inherit;
    font-size: var(--text-sm);
    line-height: 1.6;
    white-space: pre-wrap;
  }

  .result {
    gap: 8px;
    padding: 11px 12px;
    color: var(--accent);
    font-size: var(--text-sm);
    background: var(--accent-soft);
    border-top: 1px solid color-mix(in srgb, var(--accent) 11%, transparent);
  }

  .timeline { padding-left: 34px; }

  .timeline article {
    position: relative;
    min-height: 63px;
    padding: 0 0 16px 19px;
    border-left: 1px solid var(--line);
  }

  .timeline article:last-child { border-left-color: transparent; }

  .timeline-dot {
    position: absolute;
    top: 2px;
    left: -5px;
    width: 9px;
    height: 9px;
    background: var(--panel-soft);
    border: 2px solid var(--muted);
    border-radius: 999px;
  }

  .decision-event .timeline-dot { border-color: var(--warning); }

  .timeline-copy > div { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; }
  .timeline-copy strong { font-size: var(--text-sm); }
  .timeline-copy time { color: var(--subtle); font-size: var(--text-xs); }
  .timeline-copy p { margin: 5px 0 0; color: var(--muted); font-size: var(--text-sm); line-height: 1.45; }

  .evidence-list { display: grid; gap: 12px; }

  .artifact {
    overflow: hidden;
    background: var(--panel-soft);
    border: 1px solid var(--line);
    border-radius: 12px;
  }

  .artifact header { gap: 9px; padding: 11px 12px; border-bottom: 1px solid var(--line); }
  .artifact header > span { display: grid; width: 30px; height: 30px; color: var(--accent); background: var(--accent-soft); border-radius: 8px; place-items: center; }
  .artifact strong, .artifact small { display: block; }
  .artifact strong { font-size: var(--text-sm); }
  .artifact small { margin-top: 3px; color: var(--subtle); font-size: var(--text-xs); }
  .artifact pre { margin: 0; padding: 13px; }

  .empty-evidence { display: grid; min-height: 240px; place-content: center; justify-items: center; text-align: center; color: var(--subtle); }
  .empty-evidence h3 { margin: 11px 0 4px; color: var(--muted); font-size: 0.8rem; }
  .empty-evidence p { margin: 0; font-size: var(--text-sm); }

  .drawer-footer {
    display: flex;
    min-height: 61px;
    flex: 0 0 auto;
    align-items: center;
    justify-content: flex-end;
    padding: 10px 18px;
    background: var(--panel);
    border-top: 1px solid var(--line);
  }

  .drawer-footer button:disabled, .decision-actions button:disabled { cursor: wait; opacity: 0.55; }

  @media (max-width: 540px) {
    .task-heading, .tab-content { padding-right: 18px; padding-left: 18px; }
    .decision-callout, .run-progress { margin-right: 18px; margin-left: 18px; }
    .edit-grid { grid-template-columns: 1fr; }
    .structured-budget .wide { grid-column: auto; }
    .provider-truth { grid-template-columns: 1fr 1fr; margin-right: 18px; margin-left: 18px; }
    .operations-grid { grid-template-columns: 1fr; }
    .task-meta { flex-direction: column; align-items: flex-start; gap: 6px; }
  }
</style>
