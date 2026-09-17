<script lang="ts">
  import { ArrowUpRight, CheckCircle2, GitPullRequestArrow, Sparkles } from '@lucide/svelte';
  import { formatDate, timeAgo } from '$lib/format';
  import type { Decision, Task } from '$lib/types';

  export let decisions: Decision[] = [];
  export let tasks: Task[] = [];
  export let onanswer: (decision: Decision, answer: string) => Promise<void>;
  export let onselect: (task: Task) => void;

  let answering = '';
  let error = '';

  function taskFor(decision: Decision): Task | undefined {
    return tasks.find((task) => task.id === decision.task_id);
  }

  async function answer(decision: Decision, option: string) {
    answering = decision.id;
    error = '';
    try {
      await onanswer(decision, option);
    } catch (caught) {
      error = caught instanceof Error ? caught.message : 'Could not record the decision.';
    } finally {
      answering = '';
    }
  }
</script>

<section class="decision-view">
  <div class="decision-intro surface">
    <div class="intro-icon"><GitPullRequestArrow size={22} /></div>
    <div>
      <span class="eyebrow">Human attention queue</span>
      <h2>{decisions.length} blocking {decisions.length === 1 ? 'decision' : 'decisions'}</h2>
      <p>Review the proposed approach, evidence, and consequences before deciding.</p>
    </div>
  </div>

  {#if error}<p class="error">{error}</p>{/if}

  <div class="decision-grid">
    {#each decisions as decision (decision.id)}
      {@const task = taskFor(decision)}
      <article class="decision-card surface">
        <header>
          <span class="waiting-badge"><span></span> Waiting {timeAgo(decision.created_at)}</span>
          {#if task}
            <button type="button" on:click={() => onselect(task)} aria-label="Open task">
              <ArrowUpRight size={16} />
            </button>
          {/if}
        </header>

        {#if task}
          <span class="task-label">{task.title}</span>
        {/if}
        <h3>{decision.question}</h3>

        {#if task?.acceptance_criteria}
          <div class="context-block">
            <span>Acceptance signal</span>
            <p class="clamp-3">{task.acceptance_criteria}</p>
          </div>
        {/if}

        <div class="recommendation">
          <Sparkles size={14} />
          Agent recommends <strong>{decision.recommendation}</strong>
        </div>

        <div class="decision-facts">
          <span><small>Risk</small><strong>{decision.risk_class}</strong></span>
          <span><small>Estimated cost</small><strong>{(decision.estimated_cost_minor / 100).toFixed(2)} {decision.currency}</strong></span>
          <span><small>Expires</small><strong>{decision.expires_at ? formatDate(decision.expires_at) : 'No expiry'}</strong></span>
        </div>
        {#if decision.evidence_summary}
          <div class="consequence"><span>Evidence</span><p>{decision.evidence_summary}</p></div>
        {/if}
        <div class="consequence-grid">
          <div class="consequence"><span>If you wait</span><p>{decision.waiting_consequence}</p></div>
          <div class="consequence"><span>If you reject</span><p>{decision.rejecting_consequence}</p></div>
        </div>

        <footer>
          {#each decision.options as option, index}
            <button
              type="button"
              class:recommended={option === decision.recommendation}
              class:reject={index > 0}
              disabled={answering === decision.id}
              on:click={() => answer(decision, option)}
            >
              {#if option === decision.recommendation}<CheckCircle2 size={15} />{/if}
              {answering === decision.id ? 'Recording…' : option}
            </button>
          {/each}
        </footer>
      </article>
    {:else}
      <div class="all-clear surface">
        <span class="clear-icon"><CheckCircle2 size={25} /></span>
        <h3>No decisions waiting</h3>
        <p>No agents are waiting for a human decision.</p>
      </div>
    {/each}
  </div>
</section>

<style>
  .decision-view {
    min-height: 0;
    flex: 1;
    padding-right: 2px;
    overflow-y: auto;
  }

  .decision-intro {
    display: flex;
    align-items: center;
    gap: 15px;
    padding: 18px;
    border-radius: 15px;
  }

  .intro-icon,
  .clear-icon {
    display: grid;
    width: 45px;
    height: 45px;
    flex: 0 0 auto;
    color: var(--warning);
    background: var(--warning-soft);
    border-radius: 13px;
    place-items: center;
  }

  .decision-intro h2 {
    margin: 4px 0 3px;
    font-size: 1.06rem;
    font-weight: 680;
  }

  .decision-intro p,
  .all-clear p {
    margin: 0;
    color: var(--muted);
    font-size: var(--text-sm);
  }

  .decision-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 380px), 1fr));
    gap: 20px;
    margin-top: 20px;
    padding-bottom: 20px;
  }

  .decision-card {
    padding: 17px;
    background: var(--panel);
    border-top: 3px solid color-mix(in srgb, var(--warning) 60%, var(--panel));
    border-radius: 15px;
  }

  .decision-card header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .waiting-badge {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--warning);
    font-size: var(--text-xs);
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
  }

  .waiting-badge span {
    width: 6px;
    height: 6px;
    background: var(--warning);
    border-radius: 999px;
    box-shadow: 0 0 0 4px var(--warning-soft);
  }

  header button {
    display: grid;
    width: 29px;
    height: 29px;
    color: var(--muted);
    background: transparent;
    border: 1px solid var(--line);
    border-radius: 8px;
    cursor: pointer;
    place-items: center;
  }

  .task-label {
    display: block;
    margin-top: 20px;
    color: var(--planning);
    font-size: var(--text-sm);
    font-weight: 650;
  }

  h3 {
    margin: 7px 0 16px;
    font-size: 0.95rem;
    font-weight: 650;
    line-height: 1.4;
  }

  .context-block {
    padding: 11px 12px;
    background: var(--panel-soft);
    border: 1px solid var(--line);
    border-radius: 10px;
  }

  .context-block span {
    color: var(--subtle);
    font-size: var(--text-xs);
    font-weight: 750;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .context-block p {
    margin: 5px 0 0;
    color: var(--muted);
    font-size: var(--text-sm);
    line-height: 1.48;
  }

  .recommendation {
    display: flex;
    align-items: center;
    gap: 7px;
    margin-top: 13px;
    color: var(--muted);
    font-size: var(--text-sm);
  }

  .recommendation :global(svg) {
    color: var(--accent);
  }

  .recommendation strong {
    color: var(--text);
  }

  .decision-facts,
  .consequence-grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
    margin-top: 12px;
  }

  .decision-facts span,
  .consequence {
    padding: 8px;
    background: var(--panel-soft);
    border: 1px solid var(--line);
    border-radius: 8px;
  }

  .decision-facts small,
  .consequence > span {
    display: block;
    color: var(--subtle);
    font-size: var(--text-xs);
    font-weight: 700;
    text-transform: uppercase;
  }

  .decision-facts strong {
    display: block;
    margin-top: 4px;
    font-size: var(--text-xs);
    text-transform: capitalize;
  }

  .consequence-grid {
    grid-template-columns: 1fr 1fr;
  }

  .consequence { margin-top: 8px; }
  .consequence p { margin: 5px 0 0; color: var(--muted); font-size: var(--text-xs); line-height: 1.4; }

  footer {
    display: flex;
    gap: 8px;
    margin-top: 16px;
  }

  footer button {
    display: inline-flex;
    min-height: 38px;
    flex: 1;
    align-items: center;
    justify-content: center;
    gap: 7px;
    padding: 0 12px;
    color: var(--muted);
    font-size: var(--text-sm);
    font-weight: 700;
    background: transparent;
    border: 1px solid var(--line-strong);
    border-radius: 9px;
    cursor: pointer;
  }

  footer button.recommended {
    color: var(--on-accent);
    background: var(--accent);
    border-color: var(--accent);
  }

  footer button:disabled {
    cursor: wait;
    opacity: 0.55;
  }

  .all-clear {
    display: grid;
    min-height: 250px;
    grid-column: 1 / -1;
    place-content: center;
    justify-items: center;
    text-align: center;
    border-radius: 15px;
  }

  .all-clear .clear-icon {
    color: var(--accent);
    background: var(--accent-soft);
  }

  .all-clear h3 {
    margin: 13px 0 5px;
  }

  .error {
    margin: 10px 0 0;
    padding: 9px 11px;
    color: var(--danger);
    font-size: var(--text-sm);
    background: var(--danger-soft);
    border-radius: 9px;
  }

  @media (max-width: 600px) {
    .decision-grid {
      grid-template-columns: 1fr;
    }

    footer {
      flex-direction: column;
    }

    .decision-facts,
    .consequence-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
