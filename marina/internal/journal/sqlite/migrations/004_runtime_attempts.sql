CREATE TABLE runtime_attempts (
    id TEXT PRIMARY KEY,
    execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
    attempt_number INTEGER NOT NULL CHECK (attempt_number > 0),
    idempotency_key TEXT NOT NULL UNIQUE,
    specification_json TEXT NOT NULL,
    specification_digest TEXT NOT NULL,
    provider_handle TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK (state IN (
        'pending', 'starting', 'running', 'paused',
        'succeeded', 'failed', 'cancelled', 'timed_out'
    )),
    heartbeat_at INTEGER,
    usage_json TEXT NOT NULL DEFAULT '{}',
    output_refs_json TEXT NOT NULL DEFAULT '[]',
    deadline_at INTEGER NOT NULL,
    cleanup_state TEXT NOT NULL DEFAULT 'pending' CHECK (cleanup_state IN (
        'pending', 'running', 'completed', 'failed'
    )),
    outputs_collected INTEGER NOT NULL DEFAULT 0 CHECK (outputs_collected IN (0, 1)),
    failure_class TEXT NOT NULL DEFAULT '',
    failure_message TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE (execution_id, attempt_number)
);

CREATE INDEX idx_runtime_attempts_execution ON runtime_attempts(execution_id, attempt_number);
CREATE INDEX idx_runtime_attempts_active ON runtime_attempts(state, cleanup_state, updated_at);

CREATE TABLE runtime_artifacts (
    id TEXT PRIMARY KEY,
    execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
    runtime_attempt_id TEXT NOT NULL REFERENCES runtime_attempts(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    uri TEXT NOT NULL,
    digest TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    created_at INTEGER NOT NULL,
    UNIQUE (runtime_attempt_id, digest)
);

CREATE INDEX idx_runtime_artifacts_execution ON runtime_artifacts(execution_id, created_at, id);
