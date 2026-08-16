CREATE TABLE operator_sessions (
    token_hash TEXT PRIMARY KEY CHECK (length(token_hash) = 64),
    csrf_hash TEXT NOT NULL CHECK (length(csrf_hash) = 64),
    operator_id TEXT NOT NULL CHECK (length(operator_id) BETWEEN 1 AND 128),
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
) WITHOUT ROWID;

CREATE INDEX idx_operator_sessions_expiry
ON operator_sessions(expires_at);
