CREATE TABLE repositories (
    id            BIGSERIAL    PRIMARY KEY,
    full_name     VARCHAR(255) NOT NULL UNIQUE,
    last_seen_tag VARCHAR(100),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE subscriptions (
    id            BIGSERIAL    PRIMARY KEY,
    repository_id BIGINT       NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    email         VARCHAR(255) NOT NULL,
    token         VARCHAR(255) NOT NULL,
    UNIQUE (repository_id, email)
);

CREATE TABLE outbox_messages (
    id         BIGSERIAL    PRIMARY KEY,
    queue      VARCHAR(255) NOT NULL,
    payload    JSONB        NOT NULL,
    status     VARCHAR(20)  NOT NULL DEFAULT 'PENDING',
    attempts   INT          NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_outbox_messages_status ON outbox_messages (status);
