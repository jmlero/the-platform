import { describe, expect, it } from 'vitest';
import { workQueues } from './work-queues';
import type { Task } from './types';

describe('operator work queues', () => {
  it('separates decisions and failures from current results and running work', () => {
    const task = (id: string, status: Task['status'], display_state: string) =>
      ({ id, status, display_state } as Task);
    const queues = workQueues([
      task('decision', 'waiting_decision', 'waiting'),
      task('failed', 'failed', 'failed'),
      task('stale', 'done', 'stale-evidence'),
      task('syncing', 'done', 'syncing'),
      task('result', 'done', 'completed'),
      task('worker', 'running', 'running'),
      task('backlog', 'backlog', 'backlog'),
      task('cancelled', 'cancelled', 'cancelled')
    ]);
    expect(queues.attention.map(t => t.id)).toEqual(['decision', 'failed', 'stale']);
    expect(queues.results.map(t => t.id)).toEqual(['result']);
    expect(queues.active.map(t => t.id)).toEqual(['syncing', 'worker']);
  });
});
