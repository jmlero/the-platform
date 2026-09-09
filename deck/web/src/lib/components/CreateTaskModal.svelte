<script lang="ts">
  import { ArrowRight, Check, ClipboardPlus, X } from '@lucide/svelte';
  import type { CreateTaskInput, Priority } from '$lib/types';

  export let onclose: () => void;
  export let oncreate: (input: CreateTaskInput) => Promise<void>;

  let title = '';
  let description = '';
  let acceptanceCriteria = '';
  let priority: Priority = 'medium';
  let budgetMinutes = 30;
  let maxAttempts = 2;
  let maxTokens = 200000;
  let maxCostMinor = 0;
  let currency = 'EUR';
  let maxConcurrency = 1;
  let authority = 'source.read';
  let allowPullRequest = false;
  let deadline = '';
  let status: 'backlog' | 'ready' = 'backlog';
  let submitting = false;
  let error = '';

  async function submit() {
    error = '';
    if (!title.trim()) {
      error = 'Give the task a clear outcome.';
      return;
    }
    submitting = true;
    try {
      await oncreate({
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
          authority: [...new Set([...authority.split(',').map((item) => item.trim()).filter(Boolean), ...(allowPullRequest ? ['source.read', 'source.write', 'pull_request.create'] : [])])]
        },
        status
      });
    } catch (caught) {
      error = caught instanceof Error ? caught.message : 'Could not create the task.';
    } finally {
      submitting = false;
    }
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') onclose();
  }
</script>

<svelte:window on:keydown={handleKeydown} />

<div class="modal-backdrop" role="presentation" on:click|self={onclose}>
  <div class="modal glass" role="dialog" aria-modal="true" aria-labelledby="create-title" tabindex="-1">
    <header>
      <div class="header-icon"><ClipboardPlus size={18} /></div>
      <div>
        <span class="eyebrow">New work item</span>
        <h2 id="create-title">Shape the outcome</h2>
      </div>
      <button class="icon-button" type="button" on:click={onclose} aria-label="Close">
        <X size={17} />
      </button>
    </header>

    <form on:submit|preventDefault={submit}>
      <label class="field field-wide">
        <span>Outcome</span>
        <input bind:value={title} placeholder="What should be true when this is done?" />
      </label>

      <label class="field field-wide">
        <span>Context</span>
        <textarea
          bind:value={description}
          rows="3"
          placeholder="Relevant background, boundaries, and constraints"
        ></textarea>
      </label>

      <label class="field field-wide">
        <span>Acceptance criteria</span>
        <textarea
          bind:value={acceptanceCriteria}
          rows="3"
          placeholder="Observable checks that prove the outcome is complete"
        ></textarea>
      </label>

      <div class="field-row">
        <label class="field">
          <span>Priority</span>
          <select bind:value={priority}>
            <option value="low">Low</option>
            <option value="medium">Medium</option>
            <option value="high">High</option>
            <option value="urgent">Urgent</option>
          </select>
        </label>

        <label class="field">
          <span>Time budget</span>
          <div class="input-suffix">
            <input bind:value={budgetMinutes} type="number" min="1" max="1440" />
            <small>minutes</small>
          </div>
        </label>
      </div>

      <label class="permission">
        <input type="checkbox" bind:checked={allowPullRequest} />
        <span>Allow Marina to change the configured repository and open a PR.<small>Stops for human review. Two attempts cover implementation and independent verification.</small></span>
      </label>

      <details class="budget-details">
        <summary>Structured limits</summary>
        <div class="budget-grid">
          <label class="field">
            <span>Deadline (optional)</span>
            <input bind:value={deadline} type="datetime-local" />
          </label>
          <label class="field">
            <span>Maximum attempts</span>
            <input bind:value={maxAttempts} type="number" min="1" />
          </label>
          <label class="field">
            <span>Maximum tokens</span>
            <input bind:value={maxTokens} type="number" min="0" />
          </label>
          <label class="field">
            <span>Maximum cost (minor units)</span>
            <input bind:value={maxCostMinor} type="number" min="0" />
          </label>
          <label class="field">
            <span>Currency</span>
            <input bind:value={currency} maxlength="3" />
          </label>
          <label class="field">
            <span>Maximum concurrency</span>
            <input bind:value={maxConcurrency} type="number" min="1" />
          </label>
          <label class="field field-wide">
            <span>Authority (comma separated)</span>
            <input bind:value={authority} placeholder="source.read, pull_request.create" />
          </label>
        </div>
      </details>

      <fieldset>
        <legend>Place task in</legend>
        <div class="destination-grid">
          <label class:chosen={status === 'backlog'}>
            <input type="radio" bind:group={status} value="backlog" />
            <span class="choice-icon"><ClipboardPlus size={15} /></span>
            <span>
              <strong>Backlog</strong>
              <small>Keep it available for refinement</small>
            </span>
            {#if status === 'backlog'}<Check class="check" size={15} />{/if}
          </label>
          <label class:chosen={status === 'ready'}>
            <input type="radio" bind:group={status} value="ready" />
            <span class="choice-icon choice-ready"><ArrowRight size={15} /></span>
            <span>
              <strong>Ready queue</strong>
              <small>An agent may claim it immediately</small>
            </span>
            {#if status === 'ready'}<Check class="check" size={15} />{/if}
          </label>
        </div>
      </fieldset>

      {#if error}<p class="form-error">{error}</p>{/if}

      <footer>
        <button class="secondary-button" type="button" on:click={onclose}>Cancel</button>
        <button class="primary-button" type="submit" disabled={submitting}>
          {submitting ? 'Creating…' : 'Create task'}
          <ArrowRight size={15} />
        </button>
      </footer>
    </form>
  </div>
</div>

<style>
  .permission { display:flex; gap:12px; align-items:flex-start; padding:14px; border:1px solid var(--line); border-radius:10px; font-size:.78rem; }
  .permission input { width:auto; margin-top:3px; }
  .permission small { display:block; margin-top:6px; color:var(--subtle); line-height:1.5; }
  .modal-backdrop {
    position: fixed;
    z-index: 80;
    inset: 0;
    display: grid;
    padding: 24px;
    background: rgba(3, 4, 6, 0.72);
    backdrop-filter: blur(8px);
    place-items: center;
  }

  .modal {
    width: min(620px, 100%);
    max-height: calc(100vh - 48px);
    overflow-y: auto;
    background: #11151a;
    border-radius: 18px;
    box-shadow: var(--shadow);
  }

  header {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 20px 22px 17px;
    border-bottom: 1px solid var(--line);
  }

  .header-icon {
    display: grid;
    width: 38px;
    height: 38px;
    color: var(--acid);
    background: var(--acid-soft);
    border-radius: 11px;
    place-items: center;
  }

  h2 {
    margin: 3px 0 0;
    font-size: 1rem;
    font-weight: 680;
  }

  .icon-button {
    display: grid;
    width: 34px;
    height: 34px;
    margin-left: auto;
    color: var(--muted);
    background: transparent;
    border: 1px solid var(--line);
    border-radius: 9px;
    cursor: pointer;
    place-items: center;
  }

  form {
    display: grid;
    gap: 17px;
    padding: 20px 22px 22px;
  }

  .field-row {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 14px;
  }

  .field {
    display: grid;
    gap: 7px;
  }

  .field > span,
  legend {
    color: var(--muted);
    font-size: 0.67rem;
    font-weight: 700;
    letter-spacing: 0.04em;
  }

  input,
  textarea,
  select {
    width: 100%;
    color: var(--text);
    background: rgba(255, 255, 255, 0.035);
    border: 1px solid var(--line);
    border-radius: 10px;
    transition: border-color 150ms ease;
  }

  input,
  select {
    height: 41px;
    padding: 0 12px;
  }

  textarea {
    padding: 10px 12px;
    line-height: 1.45;
    resize: vertical;
  }

  input:focus,
  textarea:focus,
  select:focus {
    border-color: rgba(184, 243, 107, 0.45);
    outline: none;
  }

  input::placeholder,
  textarea::placeholder {
    color: #5e6765;
  }

  .input-suffix {
    position: relative;
  }

  .input-suffix input {
    padding-right: 62px;
  }

  .input-suffix small {
    position: absolute;
    top: 50%;
    right: 12px;
    color: var(--subtle);
    font-size: 0.65rem;
    transform: translateY(-50%);
  }

  fieldset {
    padding: 0;
    border: 0;
  }

  .budget-details {
    padding: 12px;
    background: rgba(255, 255, 255, 0.02);
    border: 1px solid var(--line);
    border-radius: 11px;
  }

  .budget-details summary {
    color: var(--muted);
    font-size: 0.68rem;
    font-weight: 700;
    cursor: pointer;
  }

  .budget-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
    margin-top: 14px;
  }

  .budget-grid .field-wide {
    grid-column: 1 / -1;
  }

  legend {
    margin-bottom: 8px;
  }

  .destination-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
  }

  .destination-grid label {
    position: relative;
    display: flex;
    min-height: 70px;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    background: rgba(255, 255, 255, 0.025);
    border: 1px solid var(--line);
    border-radius: 11px;
    cursor: pointer;
  }

  .destination-grid label.chosen {
    background: var(--acid-soft);
    border-color: rgba(184, 243, 107, 0.28);
  }

  .destination-grid input {
    position: absolute;
    width: 1px;
    height: 1px;
    opacity: 0;
  }

  .choice-icon {
    display: grid;
    width: 31px;
    height: 31px;
    flex: 0 0 auto;
    color: var(--muted);
    background: rgba(255, 255, 255, 0.05);
    border-radius: 9px;
    place-items: center;
  }

  .chosen .choice-icon {
    color: var(--acid);
    background: rgba(184, 243, 107, 0.14);
  }

  .destination-grid strong,
  .destination-grid small {
    display: block;
  }

  .destination-grid strong {
    font-size: 0.74rem;
  }

  .destination-grid small {
    margin-top: 3px;
    color: var(--muted);
    font-size: 0.62rem;
  }

  .destination-grid :global(.check) {
    margin-left: auto;
    color: var(--acid);
  }

  .form-error {
    margin: -4px 0 0;
    padding: 9px 11px;
    color: var(--red);
    font-size: 0.7rem;
    background: var(--red-soft);
    border-radius: 9px;
  }

  footer {
    display: flex;
    justify-content: flex-end;
    gap: 9px;
    padding-top: 3px;
  }

  .secondary-button,
  .primary-button {
    display: inline-flex;
    height: 39px;
    align-items: center;
    justify-content: center;
    gap: 7px;
    padding: 0 15px;
    font-size: 0.72rem;
    font-weight: 700;
    border-radius: 10px;
    cursor: pointer;
  }

  .secondary-button {
    color: var(--muted);
    background: transparent;
    border: 1px solid var(--line);
  }

  .primary-button {
    color: #10140c;
    background: var(--acid);
    border: 0;
  }

  .primary-button:disabled {
    cursor: wait;
    opacity: 0.55;
  }

  @media (max-width: 560px) {
    .modal-backdrop {
      padding: 0;
      place-items: end center;
    }

    .modal {
      max-height: 94vh;
      border-radius: 18px 18px 0 0;
    }

    .field-row,
    .budget-grid,
    .destination-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
