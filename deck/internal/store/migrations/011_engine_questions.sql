ALTER TABLE decisions ADD COLUMN interaction_id TEXT NOT NULL DEFAULT '';
ALTER TABLE decisions ADD COLUMN answer_mode TEXT NOT NULL DEFAULT 'options' CHECK (answer_mode IN ('options', 'text'));
CREATE UNIQUE INDEX idx_decisions_run_interaction ON decisions(run_id, interaction_id) WHERE interaction_id <> '';
