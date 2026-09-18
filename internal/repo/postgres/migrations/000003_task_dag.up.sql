CREATE TABLE agent_tasks (
    id UUID PRIMARY KEY,
    run_id UUID NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    task_type VARCHAR(64) NOT NULL,
    owner_agent_id VARCHAR(128) NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    status VARCHAR(32) NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(run_id, idempotency_key)
);

CREATE INDEX idx_agent_tasks_run_status ON agent_tasks(run_id, status);

CREATE TABLE agent_task_dependencies (
    task_id UUID NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
    blocked_by_task_id UUID NOT NULL REFERENCES agent_tasks(id) ON DELETE RESTRICT,
    PRIMARY KEY(task_id, blocked_by_task_id),
    CHECK (task_id <> blocked_by_task_id)
);

CREATE INDEX idx_agent_task_dependencies_blocked_by ON agent_task_dependencies(blocked_by_task_id);
