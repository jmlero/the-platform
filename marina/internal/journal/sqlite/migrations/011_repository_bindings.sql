CREATE TABLE repository_bindings (
 project_id TEXT NOT NULL,
 version INTEGER NOT NULL,
 template_digest TEXT NOT NULL,
 document_json TEXT NOT NULL,
 PRIMARY KEY(project_id,version)
);
CREATE TABLE repository_receipts (
 project_id TEXT NOT NULL,
 request_revision INTEGER NOT NULL,
 result_json TEXT NOT NULL,
 PRIMARY KEY(project_id,request_revision)
);
