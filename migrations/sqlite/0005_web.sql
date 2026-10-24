-- Web UI (#102, docs/WEB-DESIGN.md). Times are INTEGER Unix seconds (UTC).

-- One row per web session. id is the hex SHA-256 of the token: the table
-- never holds the token itself. expires_at slides for browser sessions and never
-- passes max_at.
CREATE TABLE web_sessions (
  id         TEXT    PRIMARY KEY,
  user_id    INTEGER NOT NULL,
  kind       TEXT    NOT NULL CHECK (kind IN ('miniapp','browser')),
  device     TEXT    NOT NULL DEFAULT '', -- User-Agent, at most 120 characters
  created_at INTEGER NOT NULL,
  seen_at    INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  max_at     INTEGER NOT NULL
);
CREATE INDEX web_sessions_user ON web_sessions (user_id, kind);
CREATE INDEX web_sessions_expires ON web_sessions (expires_at);

-- A login code that the Mini App made for one user (§3.3). code_hash is
-- the hex SHA-256 of the normalised code; the table never holds the code.
-- A redeem deletes the row, so each code works one time.
CREATE TABLE web_login_codes (
  code_hash  TEXT    PRIMARY KEY,
  user_id    INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE INDEX web_login_codes_user ON web_login_codes (user_id);
CREATE INDEX web_login_codes_expires ON web_login_codes (expires_at);

-- Login Widget proofs that the server accepted (§3.4). hash is the hex
-- SHA-256 of the widget hash. A second use of one proof fails.
CREATE TABLE web_used_proofs (
  hash       TEXT    PRIMARY KEY,
  expires_at INTEGER NOT NULL
);
CREATE INDEX web_used_proofs_expires ON web_used_proofs (expires_at);

-- What the web knows about a user in a group. Only getChatMember writes
-- 'member' or 'left'; an update seen in the group writes 'unknown' (a hint,
-- never access). No foreign keys, as for drafts: a group can have no chats
-- row yet, and nothing must stop MigrateChat.
CREATE TABLE chat_members (
  chat_id    INTEGER NOT NULL,
  user_id    INTEGER NOT NULL,
  status     TEXT    NOT NULL CHECK (status IN ('member','left','unknown')),
  seen_at    INTEGER NOT NULL DEFAULT 0, -- last update from the user in the chat
  checked_at INTEGER NOT NULL DEFAULT 0, -- last getChatMember answer; 0 = never
  PRIMARY KEY (chat_id, user_id)
);
CREATE INDEX chat_members_user ON chat_members (user_id, status);
