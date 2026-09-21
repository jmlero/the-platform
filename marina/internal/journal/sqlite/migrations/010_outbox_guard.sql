-- Preserve the exact mutation guard across ambiguous responses and restarts.
ALTER TABLE provider_outbox ADD COLUMN guard_json TEXT NOT NULL DEFAULT '';
