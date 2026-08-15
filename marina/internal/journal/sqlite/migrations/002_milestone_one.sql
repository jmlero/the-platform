ALTER TABLE executions ADD COLUMN project_id TEXT NOT NULL DEFAULT 'prj_default';
ALTER TABLE executions ADD COLUMN external_run_version INTEGER NOT NULL DEFAULT 1 CHECK (external_run_version > 0);
ALTER TABLE executions ADD COLUMN fence_token INTEGER NOT NULL DEFAULT 1 CHECK (fence_token > 0);
ALTER TABLE executions ADD COLUMN task_version INTEGER NOT NULL DEFAULT 1 CHECK (task_version > 0);
ALTER TABLE executions ADD COLUMN task_digest TEXT NOT NULL DEFAULT '';
ALTER TABLE executions ADD COLUMN binding_version INTEGER NOT NULL DEFAULT 1 CHECK (binding_version > 0);
ALTER TABLE executions ADD COLUMN command_cursor INTEGER NOT NULL DEFAULT 0 CHECK (command_cursor >= 0);
ALTER TABLE executions ADD COLUMN retry_attempt INTEGER NOT NULL DEFAULT 0 CHECK (retry_attempt >= 0);
ALTER TABLE executions ADD COLUMN next_eligible_at INTEGER;
ALTER TABLE executions ADD COLUMN last_error_class TEXT NOT NULL DEFAULT '';
ALTER TABLE executions ADD COLUMN exhaustion_reason TEXT NOT NULL DEFAULT '';

CREATE TABLE installation_identity (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    installation_id TEXT NOT NULL UNIQUE,
    logical_agent_id TEXT NOT NULL UNIQUE,
    created_at INTEGER NOT NULL
);

CREATE TABLE project_bindings (
    project_id TEXT PRIMARY KEY,
    version INTEGER NOT NULL CHECK (version > 0),
    provider TEXT NOT NULL,
    repository TEXT NOT NULL,
    runtime TEXT NOT NULL,
    policy_version TEXT NOT NULL,
    context_config TEXT NOT NULL,
    artifact_config TEXT NOT NULL,
    delivery_config TEXT NOT NULL,
    budget_minutes INTEGER NOT NULL CHECK (budget_minutes > 0),
    max_concurrency INTEGER NOT NULL CHECK (max_concurrency > 0),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE acquisition_intents (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES project_bindings(project_id),
    work_id TEXT NOT NULL,
    task_version INTEGER NOT NULL CHECK (task_version > 0),
    task_digest TEXT NOT NULL,
    claim_key TEXT NOT NULL UNIQUE,
    agent_id TEXT NOT NULL,
    lease_seconds INTEGER NOT NULL CHECK (lease_seconds > 0),
    budget_minutes INTEGER NOT NULL CHECK (budget_minutes > 0),
    status TEXT NOT NULL CHECK (status IN ('pending', 'claimed', 'bound', 'exhausted')),
    external_run_id TEXT NOT NULL DEFAULT '',
    external_version INTEGER NOT NULL DEFAULT 0 CHECK (external_version >= 0),
    fence_token INTEGER NOT NULL DEFAULT 0 CHECK (fence_token >= 0),
    lease_expires_at INTEGER,
    retry_attempt INTEGER NOT NULL DEFAULT 0 CHECK (retry_attempt >= 0),
    next_eligible_at INTEGER,
    last_error_class TEXT NOT NULL DEFAULT '',
    exhaustion_reason TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE (project_id, work_id, task_version)
);

CREATE INDEX idx_acquisition_unresolved ON acquisition_intents(status, next_eligible_at, updated_at);

CREATE TABLE provider_outbox (
    id TEXT PRIMARY KEY,
    execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    kind TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    payload_json TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'published', 'exhausted')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_eligible_at INTEGER,
    last_error_class TEXT NOT NULL DEFAULT '',
    exhaustion_reason TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    published_at INTEGER,
    UNIQUE (execution_id, sequence)
);

CREATE INDEX idx_outbox_ready ON provider_outbox(status, next_eligible_at, created_at);
