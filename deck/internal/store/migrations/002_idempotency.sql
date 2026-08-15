CREATE TABLE idempotency_records (
    project_id TEXT NOT NULL REFERENCES projects(id),
    actor_id TEXT NOT NULL,
    operation TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    request_hash TEXT NOT NULL CHECK (length(request_hash) = 64),
    result_json TEXT,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (project_id, actor_id, operation, idempotency_key),
    CHECK (length(actor_id) BETWEEN 1 AND 160),
    CHECK (length(operation) BETWEEN 1 AND 80),
    CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    CHECK (length(resource_id) BETWEEN 1 AND 160)
) WITHOUT ROWID;

CREATE INDEX idx_idempotency_resource
ON idempotency_records(operation, resource_id);
