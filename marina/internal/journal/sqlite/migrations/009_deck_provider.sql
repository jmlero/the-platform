-- Product naming does not create a new binding revision or execution identity.
UPDATE project_bindings SET provider = 'deck' WHERE provider = 'appdeck';
UPDATE executions SET work_provider = 'deck' WHERE work_provider = 'appdeck';
