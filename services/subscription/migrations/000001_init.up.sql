CREATE TABLE users (
    id         BIGSERIAL    PRIMARY KEY,
    email      VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE repositories (
    id            BIGSERIAL    PRIMARY KEY,
    full_name     VARCHAR(255) NOT NULL UNIQUE,
    last_seen_tag VARCHAR(100),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE subscriptions (
    id            BIGSERIAL   PRIMARY KEY,
    user_id       BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    repository_id BIGINT      NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    token         TEXT        NOT NULL UNIQUE,
    is_confirmed  BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, repository_id)
);

CREATE INDEX idx_subscriptions_repository_id ON subscriptions (repository_id);

CREATE TABLE subscription_sagas (
    id              BIGSERIAL   PRIMARY KEY,
    subscription_id BIGINT      NOT NULL,
    status          VARCHAR(50) NOT NULL DEFAULT 'STARTED',
    current_step    VARCHAR(50) NOT NULL DEFAULT 'SEND_CONFIRMATION',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
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
