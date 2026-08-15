CREATE UNIQUE INDEX idx_agents_installation_identity
ON agents(installation_id)
WHERE installation_id <> '';
