export type TaskStatus =
  | 'backlog'
  | 'ready'
  | 'planning'
  | 'waiting_decision'
  | 'running'
  | 'verifying'
  | 'done'
  | 'blocked'
  | 'failed'
  | 'cancelled';

export type Priority = 'low' | 'medium' | 'high' | 'urgent';

export interface Task {
  id: string;
  project_id: string;
  title: string;
  description: string;
  acceptance_criteria: string;
  status: TaskStatus;
  priority: Priority;
  budget_minutes: number;
  assigned_agent_id?: string;
  version: number;
  created_at: number;
  updated_at: number;
}

export interface CreateTaskInput {
  title: string;
  description: string;
  acceptance_criteria: string;
  status: 'backlog' | 'ready';
  priority: Priority;
  budget_minutes: number;
}

export interface UpdateTaskInput {
  title?: string;
  description?: string;
  acceptance_criteria?: string;
  priority?: Priority;
  budget_minutes?: number;
  version?: number;
}

export interface Agent {
  id: string;
  name: string;
  kind: string;
  status: 'idle' | 'active' | 'waiting' | 'offline';
  capabilities: string[];
  last_seen_at: number;
  created_at: number;
}

export interface Run {
  id: string;
  task_id: string;
  agent_id: string;
  status: string;
  plan: string;
  result_summary: string;
  lease_expires_at?: number;
  started_at: number;
  finished_at?: number;
  updated_at: number;
}

export interface Decision {
  id: string;
  task_id: string;
  run_id?: string;
  question: string;
  options: string[];
  recommendation: string;
  status: 'pending' | 'answered' | 'cancelled';
  answer: string;
  created_at: number;
  answered_at?: number;
}

export interface Artifact {
  id: string;
  task_id: string;
  run_id: string;
  name: string;
  kind: string;
  uri: string;
  content: string;
  created_at: number;
}

export interface AppEvent {
  id: number;
  task_id?: string;
  run_id?: string;
  agent_id?: string;
  type: string;
  message: string;
  payload: Record<string, unknown> | null;
  created_at: number;
}

export interface TaskDetail {
  task: Task;
  runs: Run[];
  events: AppEvent[];
  decisions: Decision[];
  artifacts: Artifact[];
}

export interface Bootstrap {
  tasks: Task[];
  agents: Agent[];
  decisions: Decision[];
  events: AppEvent[];
  now: number;
}
