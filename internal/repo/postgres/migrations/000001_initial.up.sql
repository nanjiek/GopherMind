CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    username VARCHAR(64) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(32) NOT NULL DEFAULT 'user',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE refresh_tokens (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    token_jti VARCHAR(64) NOT NULL UNIQUE,
    token_hash VARCHAR(128) NOT NULL,
    device_id VARCHAR(128) NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_tokens_expires_at ON refresh_tokens(expires_at);

CREATE TABLE sessions (
    id UUID PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
    user_id VARCHAR(64) NOT NULL,
    patient_id VARCHAR(64),
    title VARCHAR(255) NOT NULL,
    model_pref VARCHAR(32) NOT NULL DEFAULT '',
    last_message_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_sessions_user_updated ON sessions(tenant_id, user_id, last_message_at DESC);

CREATE TABLE messages (
    id BIGSERIAL PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    user_id VARCHAR(64) NOT NULL,
    role VARCHAR(16) NOT NULL,
    content TEXT NOT NULL,
    request_id VARCHAR(64) NOT NULL DEFAULT '',
    provider VARCHAR(64) NOT NULL DEFAULT '',
    model_name VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_messages_session_time ON messages(session_id, created_at, id);
CREATE UNIQUE INDEX uq_messages_user_request_role ON messages(user_id, request_id, role) WHERE request_id <> '';

CREATE TABLE consumer_inbox (
    id BIGSERIAL PRIMARY KEY,
    consumer VARCHAR(64) NOT NULL,
    message_id VARCHAR(128) NOT NULL,
    status VARCHAR(32) NOT NULL,
    retry_count INTEGER NOT NULL DEFAULT 0,
    last_error VARCHAR(1024) NOT NULL DEFAULT '',
    processed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(consumer, message_id)
);
CREATE INDEX idx_consumer_inbox_status ON consumer_inbox(status);

CREATE TABLE documents (
    id UUID PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    job_id VARCHAR(36) NOT NULL DEFAULT '',
    file_key VARCHAR(512) NOT NULL,
    filename VARCHAR(255) NOT NULL,
    content_type VARCHAR(128) NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL,
    status VARCHAR(32) NOT NULL,
    error_message VARCHAR(1024) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_documents_user_status ON documents(user_id, status);

CREATE TABLE eval_runs (
    id UUID PRIMARY KEY,
    request_id VARCHAR(64) NOT NULL,
    trace_id VARCHAR(128) NOT NULL DEFAULT '',
    answer_correctness DOUBLE PRECISION NOT NULL,
    answer_completeness DOUBLE PRECISION NOT NULL,
    groundedness DOUBLE PRECISION NOT NULL,
    citation_support DOUBLE PRECISION NOT NULL,
    hallucination_risk DOUBLE PRECISION NOT NULL,
    medical_safety_flag BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_eval_runs_request_id ON eval_runs(request_id);

CREATE TABLE mcp_jobs (
    id UUID PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    tool_name VARCHAR(128) NOT NULL,
    status VARCHAR(32) NOT NULL,
    resume_token VARCHAR(64) NOT NULL UNIQUE,
    output TEXT NOT NULL DEFAULT '',
    error_message VARCHAR(1024) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_mcp_jobs_user_id ON mcp_jobs(user_id);

CREATE TABLE memory_records (
    id UUID PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    content TEXT NOT NULL,
    tags VARCHAR(1024) NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT true,
    source VARCHAR(32) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_memory_records_user_id ON memory_records(user_id);

CREATE TABLE event_streams (
    session_id UUID PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    tenant_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    patient_id VARCHAR(64),
    next_seq BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_event_streams_scope ON event_streams(tenant_id, user_id, session_id);

CREATE TABLE clinical_events (
    event_id UUID PRIMARY KEY,
    event_type VARCHAR(128) NOT NULL,
    schema_version INTEGER NOT NULL,
    tenant_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    patient_id VARCHAR(64),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    stream_seq BIGINT NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    correlation_id VARCHAR(128) NOT NULL DEFAULT '',
    causation_id UUID,
    actor_type VARCHAR(32) NOT NULL,
    actor_id VARCHAR(64) NOT NULL,
    payload JSONB NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(session_id, stream_seq),
    UNIQUE(tenant_id, idempotency_key)
);
CREATE INDEX idx_clinical_events_scope ON clinical_events(tenant_id, user_id, session_id, stream_seq);

CREATE TABLE projection_checkpoints (
    projection_name VARCHAR(128) NOT NULL,
    shard INTEGER NOT NULL,
    last_event_id UUID,
    last_recorded_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(projection_name, shard)
);

CREATE TABLE outbox_messages (
    id UUID PRIMARY KEY,
    operation_id VARCHAR(128) NOT NULL UNIQUE,
    topic VARCHAR(128) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    last_error VARCHAR(1024) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_outbox_delivery ON outbox_messages(status, available_at);

CREATE TABLE agent_runs (
    id UUID PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    session_id UUID REFERENCES sessions(id),
    status VARCHAR(32) NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE agent_steps (
    id UUID PRIMARY KEY,
    run_id UUID NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    step_seq INTEGER NOT NULL,
    status VARCHAR(32) NOT NULL,
    input JSONB NOT NULL DEFAULT '{}'::jsonb,
    output JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(run_id, step_seq)
);
