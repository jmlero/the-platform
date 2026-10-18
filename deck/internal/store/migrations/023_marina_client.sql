-- Deck submits ready work to Marina's platform API (decision 0033). Each row
-- links a Deck run to the Marina Run of the same name and records the exact
-- spec, so a lost create response is replayed instead of duplicated.
CREATE TABLE marina_runs (
    run_id TEXT PRIMARY KEY REFERENCES runs(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    namespace TEXT NOT NULL,
    spec_json TEXT NOT NULL,
    created INTEGER NOT NULL DEFAULT 0 CHECK (created IN (0, 1)),
    last_sequence INTEGER NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
    created_at INTEGER NOT NULL
);
CREATE INDEX idx_marina_runs_task ON marina_runs(task_id);

-- One Marina Turn per invoked chat message, with its frozen thread and context.
CREATE TABLE marina_turns (
    name TEXT PRIMARY KEY,
    invocation_message_id TEXT NOT NULL UNIQUE REFERENCES channel_messages(id),
    namespace TEXT NOT NULL,
    spec_json TEXT NOT NULL,
    created INTEGER NOT NULL DEFAULT 0 CHECK (created IN (0, 1)),
    finished INTEGER NOT NULL DEFAULT 0 CHECK (finished IN (0, 1)),
    created_at INTEGER NOT NULL
);

-- Catalogs published by the old provider route expire within minutes; the
-- Marina sync republishes them under its own owner.
DELETE FROM workload_catalogs;
