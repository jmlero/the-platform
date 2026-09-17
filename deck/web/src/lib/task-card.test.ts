import { describe, expect, it, vi } from 'vitest';
import { render } from 'svelte/server';
import TaskCard from './components/TaskCard.svelte';
import type { Task } from './types';

const task: Task = {
  id: 'task-design', project_id: 'prj_default', title: 'Review the proposed change',
  description: '', acceptance_criteria: '', status: 'waiting_decision', priority: 'high',
  budget_minutes: 30, budget: { time_minutes: 30, max_attempts: 2, max_tokens: 1000,
    max_cost_minor: 0, currency: 'EUR', max_concurrency: 1, authority: [] },
  version: 1, created_at: 1, updated_at: 1
};

const card = (overrides: Partial<Task>) => render(TaskCard, {
  props: { task: { ...task, ...overrides }, onselect: vi.fn() }
}).body;

describe('task status labels', () => {
  it('identifies a waiting decision even when a display-state projection is absent', () => {
    expect(card({})).toContain('Human decision needed');
  });

  it('keeps stale or synchronizing results distinct from results ready for review', () => {
    const stale = card({ status: 'done', display_state: 'stale-evidence' });
    expect(stale).toContain('Evidence is stale');
    expect(stale).not.toContain('Ready for review');
    const syncing = card({ status: 'done', display_state: 'syncing' });
    expect(syncing).toContain('Synchronizing provider state');
    expect(syncing).not.toContain('Ready for review');
    expect(card({ status: 'done', display_state: 'completed' })).toContain('Ready for review');
  });
});
