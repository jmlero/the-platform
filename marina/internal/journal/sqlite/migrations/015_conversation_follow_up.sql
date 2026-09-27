-- One follow-up model call after a turn's clarification question was answered.
ALTER TABLE conversation_turns ADD COLUMN follow_up_call TEXT NOT NULL DEFAULT '';
