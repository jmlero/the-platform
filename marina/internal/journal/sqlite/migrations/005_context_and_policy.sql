CREATE TABLE context_packages (
    digest TEXT PRIMARY KEY,
    execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
    plan_revision_id TEXT NOT NULL,
    policy_version TEXT NOT NULL,
    source_revision TEXT NOT NULL,
    manifest_json TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE (execution_id, plan_revision_id, policy_version, source_revision)
);

CREATE INDEX idx_context_packages_execution ON context_packages(execution_id, created_at, digest);

CREATE TABLE policy_snapshots (
    project_id TEXT NOT NULL,
    version TEXT NOT NULL,
    change_class TEXT NOT NULL,
    environment TEXT NOT NULL,
    risk_class TEXT NOT NULL,
    digest TEXT NOT NULL UNIQUE,
    policy_json TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (project_id, version, change_class, environment, risk_class)
) WITHOUT ROWID;

CREATE INDEX idx_policy_snapshots_project ON policy_snapshots(project_id, version);
