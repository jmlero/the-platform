CREATE TABLE repository_requests (
 project_id TEXT PRIMARY KEY REFERENCES projects(id),
 document_json TEXT NOT NULL
);
