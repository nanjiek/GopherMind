ALTER TABLE agent_tasks
    ADD COLUMN task_input JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN task_output JSONB;
