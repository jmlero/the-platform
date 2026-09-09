import type { Task } from './types';

export function workQueues(tasks: Task[]) {
  const attention: Task[] = [];
  const active: Task[] = [];
  const results: Task[] = [];
  for (const task of tasks) {
    if (['waiting_decision', 'blocked', 'failed'].includes(task.status) || task.display_state === 'stale-evidence') {
      attention.push(task);
    } else if (task.display_state === 'syncing' || ['ready', 'planning', 'running', 'verifying'].includes(task.status)) {
      active.push(task);
    } else if (task.status === 'done') {
      results.push(task);
    }
  }
  return { attention, active, results };
}
