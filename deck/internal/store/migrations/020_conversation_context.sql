CREATE TABLE conversation_context (
    invocation_message_id TEXT PRIMARY KEY REFERENCES channel_messages(id) ON DELETE CASCADE,
    context_json TEXT NOT NULL
);
