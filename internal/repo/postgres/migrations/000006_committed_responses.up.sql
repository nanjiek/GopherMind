CREATE TABLE agent_committed_responses (
    run_id UUID PRIMARY KEY REFERENCES agent_runs(id) ON DELETE RESTRICT,
    generation_id UUID NOT NULL UNIQUE,
    response_data JSONB NOT NULL,
    event_seq BIGINT NOT NULL CHECK (event_seq = 1),
    revision BIGINT NOT NULL CHECK (revision = 1),
    committed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
