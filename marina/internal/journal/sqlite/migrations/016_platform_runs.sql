-- Marina owns run state (decision 0033). Clients create Runs, Turns and answer
-- Decisions through marina.dev/v1; these tables are the authoritative ledger the
-- workflow claims from in-process.

-- One row per Run resource: the frozen work item and its ledger state.
CREATE TABLE platform_work (
	id TEXT PRIMARY KEY,
	namespace TEXT NOT NULL,
	name TEXT NOT NULL,
	uid TEXT NOT NULL,
	project_id TEXT NOT NULL,
	spec_digest TEXT NOT NULL,
	item_json TEXT NOT NULL,
	status TEXT NOT NULL,
	version INTEGER NOT NULL,
	next_fence INTEGER NOT NULL,
	depends_json TEXT NOT NULL DEFAULT '[]',
	context_json TEXT NOT NULL DEFAULT '{}',
	cancel_requested INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	UNIQUE (namespace, name)
);
CREATE INDEX platform_work_ready ON platform_work(status, created_at);

CREATE TABLE platform_runs (
	id TEXT PRIMARY KEY,
	work_id TEXT NOT NULL REFERENCES platform_work(id),
	agent_id TEXT NOT NULL,
	status TEXT NOT NULL,
	version INTEGER NOT NULL,
	fence_token INTEGER NOT NULL,
	task_version INTEGER NOT NULL,
	acceptance_digest TEXT NOT NULL,
	plan TEXT NOT NULL DEFAULT '',
	active_plan_revision_id TEXT NOT NULL DEFAULT '',
	result_summary TEXT NOT NULL DEFAULT '',
	landing_json TEXT NOT NULL DEFAULT '',
	evidence_ids_json TEXT NOT NULL DEFAULT '[]',
	lease_expires_at INTEGER,
	started_at INTEGER NOT NULL,
	finished_at INTEGER,
	updated_at INTEGER NOT NULL
);
CREATE INDEX platform_runs_work ON platform_runs(work_id);

-- Idempotent mutations: an exact replay returns the stored result.
CREATE TABLE platform_mutations (
	key TEXT PRIMARY KEY,
	operation TEXT NOT NULL,
	target TEXT NOT NULL,
	request_digest TEXT NOT NULL,
	result_json TEXT NOT NULL,
	created_at INTEGER NOT NULL
);

CREATE TABLE platform_plan_revisions (
	id TEXT PRIMARY KEY,
	run_id TEXT NOT NULL REFERENCES platform_runs(id),
	revision INTEGER NOT NULL,
	revision_json TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	UNIQUE (run_id, revision)
);

-- Plan approvals and clarification questions; each becomes a Decision resource.
CREATE TABLE platform_decisions (
	id TEXT PRIMARY KEY,
	namespace TEXT NOT NULL,
	name TEXT NOT NULL,
	owner_kind TEXT NOT NULL,
	owner_id TEXT NOT NULL,
	run_id TEXT NOT NULL DEFAULT '',
	decision_json TEXT NOT NULL,
	status TEXT NOT NULL,
	answer TEXT NOT NULL DEFAULT '',
	answered_by TEXT NOT NULL DEFAULT '',
	answered_at INTEGER,
	expires_at INTEGER,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	UNIQUE (namespace, name)
);
CREATE INDEX platform_decisions_owner ON platform_decisions(owner_id, created_at);

CREATE TABLE platform_evidence (
	id TEXT PRIMARY KEY,
	run_id TEXT NOT NULL REFERENCES platform_runs(id),
	evidence_json TEXT NOT NULL,
	created_at INTEGER NOT NULL
);

CREATE TABLE platform_commands (
	id TEXT PRIMARY KEY,
	run_id TEXT NOT NULL REFERENCES platform_runs(id),
	sequence INTEGER NOT NULL,
	type TEXT NOT NULL,
	status TEXT NOT NULL,
	reason TEXT NOT NULL,
	requested_by TEXT NOT NULL,
	result TEXT NOT NULL DEFAULT '',
	run_version INTEGER NOT NULL,
	fence_token INTEGER NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	UNIQUE (run_id, sequence)
);

-- Ordered client-facing events of a Run (owner = work id) or a Turn.
CREATE TABLE platform_events (
	owner_id TEXT NOT NULL,
	sequence INTEGER NOT NULL,
	type TEXT NOT NULL,
	message TEXT NOT NULL,
	fields_json TEXT NOT NULL DEFAULT '{}',
	producer_key TEXT NOT NULL DEFAULT '',
	occurred_at INTEGER NOT NULL,
	PRIMARY KEY (owner_id, sequence)
);
CREATE UNIQUE INDEX platform_events_producer ON platform_events(owner_id, producer_key) WHERE producer_key <> '';

-- One row per Turn resource. seq orders the invocation feed.
CREATE TABLE platform_turns (
	seq INTEGER PRIMARY KEY AUTOINCREMENT,
	id TEXT NOT NULL UNIQUE,
	namespace TEXT NOT NULL,
	name TEXT NOT NULL,
	uid TEXT NOT NULL,
	project_id TEXT NOT NULL,
	spec_digest TEXT NOT NULL,
	spec_json TEXT NOT NULL,
	status TEXT NOT NULL,
	outcome_json TEXT NOT NULL DEFAULT '',
	cancel_requested INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	UNIQUE (namespace, name)
);

-- A Project resource's repository request and Marina's latest result.
CREATE TABLE platform_projects (
	project_id TEXT PRIMARY KEY,
	namespace TEXT NOT NULL,
	name TEXT NOT NULL,
	revision INTEGER NOT NULL,
	request_json TEXT NOT NULL,
	status TEXT NOT NULL,
	result_json TEXT NOT NULL DEFAULT '',
	updated_at INTEGER NOT NULL,
	UNIQUE (namespace, name)
);

-- The workload profiles each installed project offers, for Capabilities.
CREATE TABLE platform_workloads (
	project_id TEXT PRIMARY KEY,
	catalog_json TEXT NOT NULL,
	expires_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
