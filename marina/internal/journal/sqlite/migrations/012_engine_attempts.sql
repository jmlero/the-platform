CREATE TABLE engine_attempts (
    attempt_id TEXT PRIMARY KEY REFERENCES runtime_attempts(id),
    record_json TEXT NOT NULL,
    version INTEGER NOT NULL CHECK (version > 0)
);
