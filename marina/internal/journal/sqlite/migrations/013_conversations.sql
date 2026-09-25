CREATE TABLE conversation_dispatch_cursors (
    project_id TEXT PRIMARY KEY,
    event_id INTEGER NOT NULL CHECK (event_id >= 0)
);

CREATE TABLE conversation_turns (
    message_id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    thread_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    request_id TEXT NOT NULL UNIQUE,
    body TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'completed', 'blocked')),
    specification_json TEXT NOT NULL DEFAULT '',
    specification_digest TEXT NOT NULL DEFAULT '',
    result_digest TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    deadline_at INTEGER NOT NULL,
    max_tokens INTEGER NOT NULL CHECK (max_tokens > 0),
    reserved_tokens INTEGER NOT NULL DEFAULT 0 CHECK (reserved_tokens >= 0),
    used_tokens INTEGER NOT NULL DEFAULT 0 CHECK (used_tokens >= 0),
    model_call_status TEXT NOT NULL DEFAULT '' CHECK (model_call_status IN ('', 'reserved', 'completed')),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX idx_conversation_turns_active ON conversation_turns(status, created_at);

CREATE TABLE conversation_engine_attempts (
    attempt_id TEXT PRIMARY KEY REFERENCES conversation_turns(message_id) ON DELETE CASCADE,
    record_json TEXT NOT NULL,
    version INTEGER NOT NULL CHECK (version > 0)
);

CREATE TABLE conversation_artifacts (
    id TEXT PRIMARY KEY,
    attempt_id TEXT NOT NULL REFERENCES conversation_turns(message_id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    uri TEXT NOT NULL,
    digest TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    created_at INTEGER NOT NULL,
    UNIQUE(attempt_id, digest)
);
