-- Multi-node coordination (ARCHITECTURE section 9). Times are INTEGER Unix seconds (UTC).
ALTER TABLE reminders ADD COLUMN claim_token TEXT;
ALTER TABLE reminders ADD COLUMN claim_until INTEGER NOT NULL DEFAULT 0;

ALTER TABLE chats ADD COLUMN digest_claim_token TEXT;
ALTER TABLE chats ADD COLUMN digest_claim_until INTEGER NOT NULL DEFAULT 0;

CREATE TABLE leases (
  name       TEXT    PRIMARY KEY,
  holder     TEXT    NOT NULL,
  held_until INTEGER NOT NULL,
  epoch      INTEGER NOT NULL
);

CREATE TABLE prompts (
  chat_id    INTEGER NOT NULL,
  msg_id     INTEGER NOT NULL,
  user_id    INTEGER NOT NULL,
  kind       TEXT    NOT NULL CHECK (kind IN ('edit','remind')),
  todo_id    INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  PRIMARY KEY (chat_id, msg_id)
);
CREATE INDEX prompts_expires ON prompts (expires_at);

CREATE TABLE processed_updates (
  update_id INTEGER PRIMARY KEY,
  seen_at   INTEGER NOT NULL
);
CREATE INDEX processed_updates_seen ON processed_updates (seen_at);
