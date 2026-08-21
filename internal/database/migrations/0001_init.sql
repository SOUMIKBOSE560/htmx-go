-- Pageturner initial schema
CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX sessions_user_idx ON sessions(user_id);
CREATE INDEX sessions_expires_idx ON sessions(expires_at);

CREATE TABLE books (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    author      TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'to_read'
                CHECK (status IN ('to_read', 'reading', 'finished')),
    rating      INT CHECK (rating BETWEEN 1 AND 5),
    review      TEXT,
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at TEXT
);

CREATE INDEX books_user_idx ON books(user_id);
CREATE INDEX books_user_status_idx ON books(user_id, status);

CREATE TABLE reading_goals (
    user_id    BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    year       INT NOT NULL,
    goal       INT NOT NULL DEFAULT 10 CHECK (goal BETWEEN 1 AND 500),
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
