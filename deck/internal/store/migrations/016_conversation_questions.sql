CREATE TABLE conversation_questions (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    channel_id TEXT NOT NULL REFERENCES channels(id),
    thread_id TEXT NOT NULL REFERENCES conversation_threads(id),
    invocation_message_id TEXT NOT NULL REFERENCES channel_messages(id),
    agent_id TEXT NOT NULL REFERENCES agents(id),
    question_id TEXT NOT NULL,
    message_id TEXT NOT NULL UNIQUE REFERENCES channel_messages(id),
    text TEXT NOT NULL,
    choices_json TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'answered')),
    answer_message_id TEXT UNIQUE REFERENCES channel_messages(id),
    answer TEXT NOT NULL DEFAULT '',
    answer_request_id TEXT UNIQUE,
    answered_by TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    answered_at INTEGER,
    UNIQUE(invocation_message_id, question_id)
);
CREATE INDEX idx_conversation_questions_thread ON conversation_questions(thread_id, created_at);
