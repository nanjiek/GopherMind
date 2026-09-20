CREATE TABLE session_compactions (
    tenant_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    source_seq BIGINT NOT NULL CHECK (source_seq >= 0),
    summary JSONB NOT NULL,
    input_tokens INTEGER NOT NULL CHECK (input_tokens >= 0),
    reserved_output_tokens INTEGER NOT NULL CHECK (reserved_output_tokens > 0),
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, user_id, session_id)
);
CREATE INDEX idx_session_compactions_scope ON session_compactions(tenant_id, user_id, session_id, source_seq);
