CREATE TABLE model_calls (
  id TEXT PRIMARY KEY,
  execution_id TEXT NOT NULL REFERENCES executions(id),
  attempt_id TEXT NOT NULL,
  reserved_tokens INTEGER NOT NULL CHECK(reserved_tokens > 0),
  reserved_cost_micros INTEGER NOT NULL CHECK(reserved_cost_micros >= 0),
  used_tokens INTEGER,
  used_cost_micros INTEGER,
  created_at INTEGER NOT NULL,
  finished_at INTEGER
);
CREATE INDEX model_calls_attempt ON model_calls(attempt_id);
