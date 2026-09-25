ALTER TABLE conversation_threads ADD COLUMN selected_agent_id TEXT NOT NULL DEFAULT '';
ALTER TABLE channel_messages ADD COLUMN invoked_agent_id TEXT NOT NULL DEFAULT '';
ALTER TABLE channel_messages ADD COLUMN invocation_request_id TEXT;
CREATE UNIQUE INDEX idx_channel_invocation_request ON channel_messages(invocation_request_id) WHERE invocation_request_id IS NOT NULL;
