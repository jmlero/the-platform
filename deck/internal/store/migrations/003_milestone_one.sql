ALTER TABLE tasks ADD COLUMN next_fence_token INTEGER NOT NULL DEFAULT 1 CHECK (next_fence_token > 0);

ALTER TABLE agents ADD COLUMN installation_id TEXT NOT NULL DEFAULT '';

ALTER TABLE runs ADD COLUMN version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0);
ALTER TABLE runs ADD COLUMN fence_token INTEGER NOT NULL DEFAULT 1 CHECK (fence_token > 0);
ALTER TABLE runs ADD COLUMN task_version INTEGER NOT NULL DEFAULT 1 CHECK (task_version > 0);
ALTER TABLE runs ADD COLUMN acceptance_criteria_digest TEXT NOT NULL DEFAULT '';

UPDATE tasks
SET next_fence_token = 2
WHERE EXISTS (SELECT 1 FROM runs WHERE runs.task_id = tasks.id);

CREATE TABLE commands (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id),
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    type TEXT NOT NULL CHECK (type IN ('pause', 'resume', 'cancel', 'replan', 'retry', 'reduce_authority')),
    status TEXT NOT NULL CHECK (status IN ('pending', 'acknowledged', 'rejected', 'completed')),
    reason TEXT NOT NULL DEFAULT '',
    requested_by TEXT NOT NULL,
    result TEXT NOT NULL DEFAULT '',
    run_version INTEGER NOT NULL CHECK (run_version > 0),
    fence_token INTEGER NOT NULL CHECK (fence_token > 0),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE (run_id, sequence)
);

CREATE INDEX idx_commands_run_sequence ON commands(run_id, sequence);
CREATE INDEX idx_commands_status ON commands(status, created_at);

CREATE TABLE projected_events (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id),
    producer TEXT NOT NULL,
    producer_event_id TEXT NOT NULL,
    execution_id TEXT NOT NULL,
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    type TEXT NOT NULL,
    message TEXT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '{}',
    occurred_at INTEGER NOT NULL,
    ingested_at INTEGER NOT NULL,
    UNIQUE (producer, producer_event_id),
    UNIQUE (producer, execution_id, sequence)
);

CREATE INDEX idx_projected_events_run ON projected_events(run_id, ingested_at);
