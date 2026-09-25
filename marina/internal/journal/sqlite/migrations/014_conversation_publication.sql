ALTER TABLE conversation_turns ADD COLUMN published_message_id TEXT NOT NULL DEFAULT '';
ALTER TABLE conversation_turns ADD COLUMN published_task_id TEXT NOT NULL DEFAULT '';
ALTER TABLE conversation_turns ADD COLUMN publication_json TEXT NOT NULL DEFAULT '';
