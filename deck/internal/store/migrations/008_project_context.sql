CREATE TABLE context_sources (
    project_id TEXT NOT NULL REFERENCES projects(id),
    id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    document_json TEXT NOT NULL,
    revoked_at INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (project_id, id, revision)
);
ALTER TABLE tasks ADD COLUMN context_json TEXT NOT NULL DEFAULT 'null';
CREATE TABLE run_context (
    run_id TEXT PRIMARY KEY REFERENCES runs(id),
    context_json TEXT NOT NULL
);
