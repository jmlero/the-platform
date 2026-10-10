ALTER TABLE conversation_questions ADD COLUMN expires_at INTEGER;
ALTER TABLE conversation_questions ADD COLUMN closed_at INTEGER;

-- Historical pending questions must not revive turns that already ended.
UPDATE conversation_questions SET closed_at = (
    SELECT created_at FROM conversation_outcomes
    WHERE invocation_message_id = conversation_questions.invocation_message_id
) WHERE status = 'pending' AND invocation_message_id IN (
    SELECT invocation_message_id FROM conversation_outcomes
);
