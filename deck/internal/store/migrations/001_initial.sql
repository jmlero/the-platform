CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);

CREATE TABLE agents (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'idle',
    capabilities_json TEXT NOT NULL DEFAULT '[]',
    last_seen_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    acceptance_criteria TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN (
        'backlog', 'ready', 'planning', 'waiting_decision', 'running',
        'verifying', 'done', 'blocked', 'failed', 'cancelled'
    )),
    priority TEXT NOT NULL CHECK (priority IN ('low', 'medium', 'high', 'urgent')),
    budget_minutes INTEGER NOT NULL DEFAULT 30 CHECK (budget_minutes > 0),
    assigned_agent_id TEXT REFERENCES agents(id),
    version INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE runs (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    agent_id TEXT NOT NULL REFERENCES agents(id),
    status TEXT NOT NULL CHECK (status IN (
        'planning', 'waiting_decision', 'running', 'verifying',
        'completed', 'blocked', 'failed', 'cancelled'
    )),
    plan TEXT NOT NULL DEFAULT '',
    result_summary TEXT NOT NULL DEFAULT '',
    lease_expires_at INTEGER,
    started_at INTEGER NOT NULL,
    finished_at INTEGER,
    updated_at INTEGER NOT NULL
);

CREATE TABLE decisions (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT REFERENCES runs(id),
    question TEXT NOT NULL,
    options_json TEXT NOT NULL DEFAULT '[]',
    recommendation TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('pending', 'answered', 'cancelled')),
    answer TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    answered_at INTEGER
);

CREATE TABLE artifacts (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT NOT NULL REFERENCES runs(id),
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    uri TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);

CREATE TABLE events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT REFERENCES tasks(id),
    run_id TEXT REFERENCES runs(id),
    agent_id TEXT REFERENCES agents(id),
    type TEXT NOT NULL,
    message TEXT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL
);

CREATE INDEX idx_tasks_project_status ON tasks(project_id, status, updated_at DESC);
CREATE INDEX idx_runs_task ON runs(task_id, started_at DESC);
CREATE INDEX idx_runs_agent_status ON runs(agent_id, status, updated_at DESC);
CREATE INDEX idx_decisions_status ON decisions(status, created_at DESC);
CREATE INDEX idx_events_task ON events(task_id, id DESC);
CREATE INDEX idx_events_created ON events(id DESC);

INSERT INTO projects (id, name, slug, description, created_at)
VALUES (
    'prj_default',
    'AppDeck',
    'appdeck',
    'The local human-agent work control plane.',
    CAST(unixepoch('subsec') * 1000 AS INTEGER)
);
