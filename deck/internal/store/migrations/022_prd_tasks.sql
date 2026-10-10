ALTER TABLE channel_messages ADD COLUMN prd_tasks_json TEXT NOT NULL DEFAULT 'null';
ALTER TABLE tasks ADD COLUMN prd_reference_json TEXT NOT NULL DEFAULT 'null';
ALTER TABLE tasks ADD COLUMN prd_acceptance_json TEXT NOT NULL DEFAULT 'null';
