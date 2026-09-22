ALTER TABLE tasks ADD COLUMN workload_json TEXT NOT NULL DEFAULT 'null';
CREATE TABLE workload_catalogs (
 project_id TEXT PRIMARY KEY REFERENCES projects(id),
 owner_id TEXT NOT NULL,
 catalog_json TEXT NOT NULL
);
