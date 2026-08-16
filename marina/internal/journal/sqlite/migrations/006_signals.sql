CREATE TABLE validated_signals (
    id TEXT PRIMARY KEY,
    raw_signal_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    source TEXT NOT NULL,
    external_id TEXT NOT NULL,
    signal_type TEXT NOT NULL,
    classification TEXT NOT NULL,
    severity TEXT NOT NULL,
    correlation_id TEXT NOT NULL,
    dedupe_key TEXT NOT NULL UNIQUE,
    raw_json TEXT NOT NULL,
    validated_json TEXT NOT NULL,
    observed_at INTEGER NOT NULL,
    validated_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE (source, external_id)
);

CREATE INDEX validated_signals_project_correlation
    ON validated_signals (project_id, correlation_id, observed_at);
