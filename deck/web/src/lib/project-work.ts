import type { Task } from './types';

export type TaskStage = 'backlog' | 'ready' | 'in_progress' | 'done';
export const taskStages: {id:TaskStage;label:string}[] = [
 {id:'backlog',label:'Backlog'}, {id:'ready',label:'Ready'},
 {id:'in_progress',label:'In progress'}, {id:'done',label:'Done'}
];
export function taskStage(task: Task): TaskStage {
 if (task.status === 'backlog' || task.status === 'cancelled') return 'backlog';
 if (task.status === 'ready') return 'ready';
 if (task.status === 'done' && task.review && !['stale-evidence','syncing'].includes(task.display_state ?? '')) return 'done';
 return 'in_progress';
}
export function taskLabel(task:Task):string {
 if (task.display_state === 'stale-evidence') return 'Evidence needs attention';
 if (task.display_state === 'syncing') return 'Syncing';
 if (task.status === 'done') return task.review ? 'Done' : 'Awaiting review';
 if (task.status === 'waiting_decision') return 'Needs your input';
 if (task.status === 'ready' && task.depends_on?.length) return 'Ready · has dependencies';
 return ({backlog:'Backlog',ready:'Ready',planning:'Planning',running:'Working',verifying:'Verifying',blocked:'Blocked',failed:'Failed',cancelled:'Cancelled'} as Record<string,string>)[task.status] ?? task.status;
}
export function projectTasks(tasks:Task[],projectID:string):Task[] {
 return tasks.filter(task=>task.project_id === projectID && !task.archived);
}

export function workspaceQueues(tasks:Task[]) {
 const inProgress:Task[] = [];
 const ready:Task[] = [];
 const backlog:Task[] = [];
 const completed:Task[] = [];
 for (const task of tasks) {
  switch (taskStage(task)) {
   case 'ready': ready.push(task); break;
   case 'backlog': backlog.push(task); break;
   case 'done': completed.push(task); break;
   default: inProgress.push(task);
  }
 }
 return {
  inProgress,
  todo:[...ready,...backlog],
  recent:completed.sort((a,b)=>b.updated_at-a.updated_at).slice(0,5)
 };
}
