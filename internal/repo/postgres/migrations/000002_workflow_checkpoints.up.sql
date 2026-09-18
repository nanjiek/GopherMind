ALTER TABLE agent_runs
    ADD COLUMN patient_id VARCHAR(64),
    ADD COLUMN request_id VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN workflow_id VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN workflow_version VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN current_node VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN failure_kind VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN error_code VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN checkpoint_state JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE INDEX idx_agent_runs_scope ON agent_runs(tenant_id, user_id, patient_id, session_id);

CREATE TABLE agent_run_checkpoints (
    run_id UUID NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    revision BIGINT NOT NULL CHECK (revision > 0),
    status VARCHAR(32) NOT NULL,
    current_node VARCHAR(128) NOT NULL DEFAULT '',
    failure_kind VARCHAR(32) NOT NULL DEFAULT '',
    error_code VARCHAR(128) NOT NULL DEFAULT '',
    state JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(run_id, revision)
);

CREATE INDEX idx_agent_run_checkpoints_latest ON agent_run_checkpoints(run_id, revision DESC);
