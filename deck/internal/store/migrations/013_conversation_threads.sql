CREATE TABLE conversation_threads (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    channel_id TEXT NOT NULL REFERENCES channels(id),
    root_message_id TEXT NOT NULL UNIQUE REFERENCES channel_messages(id),
    created_at INTEGER NOT NULL
);
CREATE INDEX idx_threads_channel ON conversation_threads(channel_id, created_at, id);
ALTER TABLE channel_messages ADD COLUMN thread_id TEXT REFERENCES conversation_threads(id);
CREATE INDEX idx_channel_messages_thread ON channel_messages(thread_id);

-- A task has one durable origin; one conversation may produce several tasks.
CREATE TABLE thread_tasks (
    task_id TEXT PRIMARY KEY REFERENCES tasks(id),
    thread_id TEXT NOT NULL REFERENCES conversation_threads(id)
);
CREATE INDEX idx_thread_tasks_thread ON thread_tasks(thread_id);

-- Preserve existing message/task identity when upgrading a flat channel ledger.
INSERT INTO conversation_threads (id, project_id, channel_id, root_message_id, created_at)
SELECT 'thr_' || id, project_id, channel_id, id, created_at
FROM channel_messages WHERE promoted_task_id IS NOT NULL;
UPDATE channel_messages SET thread_id = 'thr_' || id WHERE promoted_task_id IS NOT NULL;
INSERT INTO thread_tasks (task_id, thread_id)
SELECT promoted_task_id, thread_id FROM channel_messages WHERE promoted_task_id IS NOT NULL;
