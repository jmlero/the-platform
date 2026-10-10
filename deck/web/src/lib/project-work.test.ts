import { describe, expect, it } from 'vitest';
import { taskStage, taskLabel, projectTasks, workspaceQueues } from './project-work';
import type { Task } from './types';

describe('project work', () => {
 it('moves an evidenced landing directly into recently landed work', () => {
  const landed = {id:'landed', status:'done',updated_at:5,landing:{run_id:'run',repository:'example/repo',base_branch:'main',commit:'a'.repeat(40),observed_at:4,evidence_ids:['review','test','landing']}} as Task;
  expect(taskStage(landed)).toBe('done');
  expect(taskLabel(landed)).toBe('Landed');
  expect(workspaceQueues([landed]).recent.map(task=>task.id)).toEqual(['landed']);
  expect(taskStage({...landed,display_state:'stale-evidence'})).toBe('in_progress');
 });
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
 it('shows ready and backlog work as to do and only accepted current results as recently completed', () => {
  const accepted = {run_id:'run',accepted_by:'human',accepted_at:1,task_version:2,source_available:false};
  const tasks = [
   {id:'backlog',status:'backlog'}, {id:'ready',status:'ready'},
   {id:'working',status:'running'}, {id:'awaiting',status:'done'},
   {id:'stale',status:'done',display_state:'stale-evidence',review:accepted},
   {id:'older',status:'done',review:accepted,updated_at:2},
   {id:'newer',status:'done',review:accepted,updated_at:3}
  ] as Task[];
  const queues = workspaceQueues(tasks);
  expect(queues.todo.map(task=>task.id)).toEqual(['ready','backlog']);
  expect(queues.inProgress.map(task=>task.id)).toEqual(['working','awaiting','stale']);
  expect(queues.recent.map(task=>task.id)).toEqual(['newer','older']);
 });
});
