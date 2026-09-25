CREATE TABLE conversation_outcomes (
    invocation_message_id TEXT PRIMARY KEY REFERENCES channel_messages(id),
    message_id TEXT NOT NULL UNIQUE REFERENCES channel_messages(id),
    task_id TEXT UNIQUE REFERENCES tasks(id),
    followup_task_id TEXT REFERENCES tasks(id),
    status TEXT NOT NULL CHECK (status IN ('completed','blocked')),
    result_digest TEXT NOT NULL,
    request_digest TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX idx_conversation_outcomes_task ON conversation_outcomes(task_id);
