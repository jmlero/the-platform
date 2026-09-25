import { describe, expect, it } from 'vitest';
import { taskStage, taskLabel, projectTasks } from './project-work';
import type { Task } from './types';

describe('project work', () => {
 it('keeps verified work in progress until a human accepts it', () => {
  const task = {status:'done'} as Task;
  expect(taskStage(task)).toBe('in_progress');
  expect(taskLabel(task)).toBe('Awaiting review');
  expect(taskStage({...task,review:{run_id:'run',accepted_by:'human',accepted_at:1,task_version:2,source_available:false}})).toBe('done');
 });
 it('does not present stale accepted evidence as Done', () => {
  expect(taskStage({status:'done',display_state:'stale-evidence',review:{accepted_at:1}} as Task)).toBe('in_progress');
 });
 it('keeps projects and archived history out of the current task list', () => {
  const tasks = [{id:'one',project_id:'p1'},{id:'two',project_id:'p2'},{id:'archived',project_id:'p1',archived:true}] as Task[];
  expect(projectTasks(tasks,'p1').map(t=>t.id)).toEqual(['one']);
 });
});
