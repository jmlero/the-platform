CREATE TABLE executions (
    id TEXT PRIMARY KEY,
    work_provider TEXT NOT NULL,
    external_work_id TEXT NOT NULL,
    external_run_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN (
        'planning', 'waiting_decision', 'running', 'verifying',
        'completed', 'blocked', 'failed', 'cancelled'
    )),
    attempt INTEGER NOT NULL CHECK (attempt > 0),
    lease_expires_at INTEGER NOT NULL,
    deadline_at INTEGER NOT NULL,
    max_attempts INTEGER NOT NULL CHECK (max_attempts > 0),
    max_tokens INTEGER NOT NULL DEFAULT 0 CHECK (max_tokens >= 0),
    max_cost_micros INTEGER NOT NULL DEFAULT 0 CHECK (max_cost_micros >= 0),
    used_tokens INTEGER NOT NULL DEFAULT 0 CHECK (used_tokens >= 0),
    used_cost_micros INTEGER NOT NULL DEFAULT 0 CHECK (used_cost_micros >= 0),
    runtime_handle TEXT NOT NULL DEFAULT '',
    failure_class TEXT NOT NULL DEFAULT '',
    failure_message TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    finished_at INTEGER,
    UNIQUE (work_provider, external_run_id)
);

CREATE TABLE checkpoints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
    checkpoint_key TEXT NOT NULL,
    phase TEXT NOT NULL,
    runtime_handle TEXT NOT NULL DEFAULT '',
    source_revision TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL,
    UNIQUE (execution_id, checkpoint_key)
);

CREATE TABLE evidence (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
    evidence_key TEXT NOT NULL,
    kind TEXT NOT NULL,
    source_revision TEXT NOT NULL,
    environment TEXT NOT NULL DEFAULT '',
    command TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL,
    uri TEXT NOT NULL DEFAULT '',
    observed_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE (execution_id, evidence_key)
);

CREATE TABLE events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
    type TEXT NOT NULL,
    message TEXT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL
);

CREATE INDEX idx_executions_active ON executions(finished_at, updated_at, id);
CREATE INDEX idx_checkpoints_execution ON checkpoints(execution_id, id DESC);
CREATE INDEX idx_evidence_execution ON evidence(execution_id, id);
CREATE INDEX idx_events_execution ON events(execution_id, id);
