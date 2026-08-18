-- Pageturner initial schema
CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_idx ON sessions(user_id);
CREATE INDEX sessions_expires_idx ON sessions(expires_at);

CREATE TABLE books (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    author      TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'to_read'
                CHECK (status IN ('to_read', 'reading', 'finished')),
    rating      INT CHECK (rating BETWEEN 1 AND 5),
    review      TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);

CREATE INDEX books_user_idx ON books(user_id);
CREATE INDEX books_user_status_idx ON books(user_id, status);

CREATE TABLE reading_goals (
    user_id    BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    year       INT NOT NULL,
    goal       INT NOT NULL DEFAULT 10 CHECK (goal BETWEEN 1 AND 500),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
