ALTER TABLE tasks ADD COLUMN deadline_at INTEGER;
ALTER TABLE tasks ADD COLUMN max_attempts INTEGER NOT NULL DEFAULT 1 CHECK (max_attempts > 0);
ALTER TABLE tasks ADD COLUMN max_tokens INTEGER NOT NULL DEFAULT 0 CHECK (max_tokens >= 0);
ALTER TABLE tasks ADD COLUMN max_cost_minor INTEGER NOT NULL DEFAULT 0 CHECK (max_cost_minor >= 0);
ALTER TABLE tasks ADD COLUMN currency TEXT NOT NULL DEFAULT 'USD' CHECK (length(currency) = 3);
ALTER TABLE tasks ADD COLUMN max_concurrency INTEGER NOT NULL DEFAULT 1 CHECK (max_concurrency > 0);
ALTER TABLE tasks ADD COLUMN authority_json TEXT NOT NULL DEFAULT '[]';

ALTER TABLE runs ADD COLUMN active_plan_revision_id TEXT;

CREATE TABLE plan_revisions (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id),
    revision INTEGER NOT NULL CHECK (revision > 0),
    plan TEXT NOT NULL,
    context_digest TEXT NOT NULL,
    risk_class TEXT NOT NULL CHECK (risk_class IN ('low', 'medium', 'high', 'critical')),
    verification_strategy TEXT NOT NULL,
    expected_side_effects_json TEXT NOT NULL DEFAULT '[]',
    rollback_approach TEXT NOT NULL,
    source_revision TEXT NOT NULL DEFAULT '',
    policy_version TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    UNIQUE (run_id, revision)
);

CREATE INDEX idx_plan_revisions_run ON plan_revisions(run_id, revision);

INSERT INTO plan_revisions (
    id, run_id, revision, plan, context_digest, risk_class,
    verification_strategy, expected_side_effects_json, rollback_approach,
    source_revision, policy_version, created_at
)
SELECT
    'pln_migrated_' || id, id, 1, plan, acceptance_criteria_digest, 'medium',
    'Verify the task acceptance criteria.', '[]',
    'Stop the run and request a revised plan.', '', '', updated_at
FROM runs
WHERE plan <> '';

UPDATE runs
SET active_plan_revision_id = 'pln_migrated_' || id
WHERE plan <> '';

ALTER TABLE decisions RENAME TO decisions_legacy;

CREATE TABLE decisions (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT REFERENCES runs(id),
    plan_revision_id TEXT REFERENCES plan_revisions(id),
    question TEXT NOT NULL,
    options_json TEXT NOT NULL DEFAULT '[]',
    recommendation TEXT NOT NULL DEFAULT '',
    risk_class TEXT NOT NULL DEFAULT 'medium',
    evidence_summary TEXT NOT NULL DEFAULT '',
    estimated_cost_minor INTEGER NOT NULL DEFAULT 0 CHECK (estimated_cost_minor >= 0),
    currency TEXT NOT NULL DEFAULT 'USD' CHECK (length(currency) = 3),
    waiting_consequence TEXT NOT NULL DEFAULT '',
    rejecting_consequence TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('pending', 'answered', 'cancelled', 'expired', 'superseded')),
    answer TEXT NOT NULL DEFAULT '',
    expires_at INTEGER,
    created_at INTEGER NOT NULL,
    answered_at INTEGER,
    answered_by TEXT NOT NULL DEFAULT ''
);

INSERT INTO decisions (
    id, task_id, run_id, plan_revision_id, question, options_json, recommendation,
    status, answer, created_at, answered_at
)
SELECT
    id, task_id, run_id,
    CASE WHEN run_id IS NULL THEN NULL ELSE 'pln_migrated_' || run_id END,
    question, options_json, recommendation,
    status, answer, created_at, answered_at
FROM decisions_legacy;

DROP TABLE decisions_legacy;

CREATE INDEX idx_decisions_status ON decisions(status, created_at DESC);
CREATE INDEX idx_decisions_plan_revision ON decisions(plan_revision_id);

CREATE TABLE evidence_records (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT NOT NULL REFERENCES runs(id),
    plan_revision_id TEXT NOT NULL REFERENCES plan_revisions(id),
    type TEXT NOT NULL,
    producer TEXT NOT NULL,
    verifier TEXT NOT NULL,
    subject_digest TEXT NOT NULL,
    source_revision TEXT NOT NULL,
    command TEXT NOT NULL,
    outcome TEXT NOT NULL CHECK (outcome IN ('passed', 'failed')),
    provenance_uri TEXT NOT NULL,
    provenance_digest TEXT NOT NULL,
    observed_at INTEGER NOT NULL,
    expires_at INTEGER,
    artifact_uri TEXT NOT NULL,
    artifact_digest TEXT NOT NULL,
    policy_version TEXT NOT NULL DEFAULT '',
    admission_status TEXT NOT NULL CHECK (admission_status IN ('current', 'stale', 'rejected', 'superseded')),
    created_at INTEGER NOT NULL
);

CREATE INDEX idx_evidence_run ON evidence_records(run_id, created_at);
CREATE INDEX idx_evidence_task_status ON evidence_records(task_id, admission_status, created_at);

CREATE TABLE channels (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    name TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE (project_id, name)
);

CREATE TABLE channel_messages (
    id TEXT PRIMARY KEY,
    channel_id TEXT NOT NULL REFERENCES channels(id),
    project_id TEXT NOT NULL REFERENCES projects(id),
    author_type TEXT NOT NULL CHECK (author_type IN ('human', 'agent', 'system')),
    author_id TEXT NOT NULL,
    body TEXT NOT NULL,
    references_json TEXT NOT NULL DEFAULT '{}',
    promoted_task_id TEXT REFERENCES tasks(id),
    created_at INTEGER NOT NULL
);

CREATE INDEX idx_channel_messages_channel ON channel_messages(channel_id, created_at, id);

CREATE TABLE operational_states (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    task_id TEXT REFERENCES tasks(id),
    run_id TEXT REFERENCES runs(id),
    environment TEXT NOT NULL,
    rollout_state TEXT NOT NULL,
    observation_started_at INTEGER,
    observation_ends_at INTEGER,
    rollback_state TEXT NOT NULL,
    policy_version TEXT NOT NULL,
    source_revision TEXT NOT NULL,
    outcome TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL,
    UNIQUE (run_id, environment)
);

CREATE INDEX idx_operational_states_task ON operational_states(task_id, updated_at);

CREATE TABLE projection_health (
    project_id TEXT NOT NULL REFERENCES projects(id),
    producer TEXT NOT NULL,
    connected_at INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    last_success_at INTEGER,
    last_occurred_at INTEGER,
    accepted_count INTEGER NOT NULL DEFAULT 0,
    stale_count INTEGER NOT NULL DEFAULT 0,
    rejected_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    degraded_dependencies_json TEXT NOT NULL DEFAULT '[]',
    PRIMARY KEY (project_id, producer)
) WITHOUT ROWID;

ALTER TABLE artifacts ADD COLUMN digest TEXT NOT NULL DEFAULT '';
ALTER TABLE artifacts ADD COLUMN size_bytes INTEGER NOT NULL DEFAULT 0 CHECK (size_bytes >= 0);
