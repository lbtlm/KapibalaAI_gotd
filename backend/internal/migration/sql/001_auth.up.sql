CREATE TABLE users (username text PRIMARY KEY, password_hash text NOT NULL, role text NOT NULL CHECK(role IN ('admin','viewer')));
CREATE TABLE sessions (id text PRIMARY KEY, username text NOT NULL REFERENCES users, revoked boolean NOT NULL DEFAULT false);
CREATE TABLE refresh_tokens (hash text PRIMARY KEY, session_id text NOT NULL REFERENCES sessions, used boolean NOT NULL DEFAULT false, expires_at timestamptz NOT NULL);
CREATE TABLE access_tokens (hash text PRIMARY KEY, session_id text NOT NULL REFERENCES sessions, expires_at timestamptz NOT NULL);
