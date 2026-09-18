ALTER TABLE agent_tasks
    ADD COLUMN lease_owner VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN lease_epoch BIGINT NOT NULL DEFAULT 0 CHECK (lease_epoch >= 0),
    ADD COLUMN lease_expires_at TIMESTAMPTZ,
    ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    ADD COLUMN error_code VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN completed_at TIMESTAMPTZ,
    ADD COLUMN deadline_at TIMESTAMPTZ NOT NULL;

CREATE INDEX idx_agent_tasks_recovery ON agent_tasks(run_id, status, lease_expires_at);

CREATE TABLE agent_mailbox_messages (
    id UUID PRIMARY KEY,
    run_id UUID NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    task_id UUID REFERENCES agent_tasks(id) ON DELETE SET NULL,
    sender_agent_id VARCHAR(128) NOT NULL,
    target_agent_id VARCHAR(128) NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(32) NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    lease_owner VARCHAR(128) NOT NULL DEFAULT '',
    lease_epoch BIGINT NOT NULL DEFAULT 0 CHECK (lease_epoch >= 0),
    lease_expires_at TIMESTAMPTZ,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    delivered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(run_id, idempotency_key)
);

CREATE INDEX idx_agent_mailbox_delivery ON agent_mailbox_messages(run_id, target_agent_id, status, lease_expires_at, created_at);
