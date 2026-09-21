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

export type DisplayState =
  | 'backlog'
  | 'ready'
  | 'planning'
  | 'waiting'
  | 'running'
  | 'retrying'
  | 'verifying'
  | 'syncing'
  | 'stale-evidence'
  | 'blocked'
  | 'completed';

export interface Budget {
  time_minutes: number;
  deadline_at?: number;
  max_attempts: number;
  max_tokens: number;
  max_cost_minor: number;
  currency: string;
  max_concurrency: number;
  authority: string[];
}

export interface Task {
  context?: TaskContext;
  id: string;
  project_id: string;
  title: string;
  description: string;
  acceptance_criteria: string;
  status: TaskStatus;
  priority: Priority;
  budget_minutes: number;
  budget: Budget;
  display_state?: DisplayState;
  assigned_agent_id?: string;
  version: number;
  created_at: number;
  updated_at: number;
}

export interface CreateTaskInput {
  context_message_ids?: string[];
  title: string;
  description: string;
  acceptance_criteria: string;
  status: 'backlog' | 'ready';
  priority: Priority;
  budget_minutes: number;
  budget?: Budget;
}

export interface UpdateTaskInput {
  context_message_ids?: string[];
  title?: string;
  description?: string;
  acceptance_criteria?: string;
  priority?: Priority;
  budget_minutes?: number;
  budget?: Budget;
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
  version: number;
  fence_token: number;
  task_version: number;
  acceptance_criteria_digest: string;
  status: string;
  plan: string;
  active_plan_revision_id?: string;
  result_summary: string;
  lease_expires_at?: number;
  started_at: number;
  finished_at?: number;
  updated_at: number;
}

export interface PlanRevision {
  id: string;
  run_id: string;
  revision: number;
  plan: string;
  context_digest: string;
  risk_class: 'low' | 'medium' | 'high' | 'critical';
  verification_strategy: string;
  expected_side_effects: string[];
  rollback_approach: string;
  source_revision?: string;
  policy_version?: string;
  created_at: number;
}

export interface Decision {
  id: string;
  task_id: string;
  run_id?: string;
  plan_revision_id?: string;
  question: string;
  options: string[];
  recommendation: string;
  risk_class: 'low' | 'medium' | 'high' | 'critical';
  evidence_summary: string;
  estimated_cost_minor: number;
  currency: string;
  waiting_consequence: string;
  rejecting_consequence: string;
  status: 'pending' | 'answered' | 'cancelled' | 'expired' | 'superseded';
  answer: string;
  expires_at?: number;
  created_at: number;
  answered_at?: number;
  answered_by?: string;
}

export interface Artifact {
  id: string;
  task_id: string;
  run_id: string;
  name: string;
  kind: string;
  uri: string;
  content: string;
  digest: string;
  size_bytes: number;
  created_at: number;
}

export interface EvidenceRecord {
  id: string;
  task_id: string;
  run_id: string;
  plan_revision_id: string;
  type: string;
  producer: string;
  verifier: string;
  subject_digest: string;
  source_revision: string;
  command: string;
  outcome: 'passed' | 'failed';
  provenance_uri: string;
  provenance_digest: string;
  observed_at: number;
  expires_at?: number;
  artifact_uri: string;
  artifact_digest: string;
  policy_version?: string;
  admission_status: 'current' | 'stale' | 'rejected' | 'superseded';
  created_at: number;
}

export interface CommandRecord {
  id: string;
  run_id: string;
  sequence: number;
  type: string;
  status: string;
  reason: string;
  requested_by: string;
  result: string;
  run_version: number;
  fence_token: number;
  created_at: number;
  updated_at: number;
}

export interface ProviderStatus {
  project_id: string;
  connectivity: 'unknown' | 'connected' | 'stale' | 'disconnected';
  last_seen_at?: number;
  sync_state: 'unknown' | 'current' | 'lagging' | 'stale' | 'rejected';
  sync_lag_millis: number;
  lease_state: 'none' | 'active' | 'expiring' | 'expired';
  lease_expires_at?: number;
  degraded_dependencies: string[];
  stale_projection_count: number;
  rejected_projection_count: number;
  last_error?: string;
}

export interface OperationalState {
  id: string;
  project_id: string;
  task_id: string;
  run_id: string;
  environment: string;
  rollout_state: string;
  observation_started_at?: number;
  observation_ends_at?: number;
  rollback_state: string;
  policy_version: string;
  source_revision: string;
  outcome?: string;
  updated_at: number;
}

export interface TrailEntry {
  id: string;
  stage: 'signal' | 'decision' | 'change' | 'delivery' | 'observation' | 'verification' | 'outcome';
  type: string;
  title: string;
  resource_id?: string;
  payload?: Record<string, unknown>;
  occurred_at: number;
}

export interface CanonicalReferences {
  task_ids?: string[];
  run_ids?: string[];
  decision_ids?: string[];
  artifact_ids?: string[];
  evidence_ids?: string[];
}

export interface Channel {
  id: string;
  project_id: string;
  name: string;
  created_at: number;
}

export interface ChannelMessage {
  id: string;
  channel_id: string;
  project_id: string;
  author_type: 'human' | 'agent' | 'system';
  author_id: string;
  body: string;
  references: CanonicalReferences;
  promoted_task_id?: string;
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
  plan_revisions: PlanRevision[];
  events: AppEvent[];
  decisions: Decision[];
  artifacts: Artifact[];
  evidence: EvidenceRecord[];
  commands: CommandRecord[];
  operational_states: OperationalState[];
  provider_status: ProviderStatus;
  display_state: DisplayState;
  trail: TrailEntry[];
}

export interface Bootstrap {
  tasks: Task[];
  agents: Agent[];
  decisions: Decision[];
  events: AppEvent[];
  channels: Channel[];
  provider_status: ProviderStatus;
  now: number;
}

export interface ContextReference { id: string; revision: number; digest: string }
export interface TaskContext {
 repository?: RepositoryBinding; sources: ContextReference[]; messages: ContextReference[]; authorized_at: number }
export interface ContextSourceInput {
  id: string; expected_revision: number; kind: 'instruction' | 'goal' | 'document' | 'git' | 'decision';
  title: string; content?: string; reference?: string; source_revision?: string; path?: string;
  trust: 'data' | 'guidance'; state: 'active' | 'inactive'; expires_at?: number; conflicts_with?: string[];
}
export interface ContextSource extends ContextSourceInput {
  project_id: string; revision: number; digest: string; created_at: number; revoked_at?: number; superseded: boolean;
}
export interface ContextMessage { id: string; channel_id: string; author_type: string; author_id: string; content: string; digest: string; trust: 'data'; created_at: number }
export interface ResolvedContext { assembly?: {context?: ContextManifest; interpreted_outcome?: {Title?: string; Description?: string; AcceptanceCriteria?: string}}; selection: TaskContext | null; sources: ContextSource[]; messages: ContextMessage[] }
export interface ContextManifest {
  digest: string; source_revision: string; policy_version: string; created_at: string;
  inputs: Array<{kind: string; content?: string; title?: string; uri: string; owner?: string; revision?: string; digest: string; trust: string}>;
  omissions?: Array<{uri: string; title: string; digest: string; reason: string}>;
}

export interface RepositoryBinding { version: number; repository: string; clone_url: string; base_branch: string; }
export interface RepositoryRequestInput { expected_revision: number; expected_binding_version: number; repository: string; base_branch: string; }
export interface RepositoryRequest extends RepositoryRequestInput { revision: number; status: '' | 'pending' | 'connected' | 'rejected'; reason?: string; binding?: RepositoryBinding; }
