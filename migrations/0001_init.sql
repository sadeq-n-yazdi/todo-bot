-- All times are INTEGER Unix seconds (UTC).
CREATE TABLE users (
  id         INTEGER PRIMARY KEY,               -- Telegram user ID
  tz         TEXT    NOT NULL,                  -- IANA time zone
  lang       TEXT    NOT NULL DEFAULT 'en' CHECK (lang IN ('en','fa')),
  calendar   TEXT    NOT NULL DEFAULT 'gregorian' CHECK (calendar IN ('gregorian','jalali')),
  created_at INTEGER NOT NULL
);

CREATE TABLE chats (
  id             INTEGER PRIMARY KEY,           -- Telegram chat ID (= user ID for private)
  kind           TEXT    NOT NULL CHECK (kind IN ('private','group')),
  title          TEXT    NOT NULL DEFAULT '',
  next_num       INTEGER NOT NULL DEFAULT 1,    -- next per-chat todo number
  digest_time    TEXT,                          -- 'HH:MM' or NULL = off
  digest_by      INTEGER,                       -- user whose TZ/locale the digest uses
  digest_next_at INTEGER,
  created_at     INTEGER NOT NULL
);
CREATE INDEX chats_digest ON chats (digest_next_at) WHERE digest_next_at IS NOT NULL;

CREATE TABLE todos (
  id         INTEGER PRIMARY KEY,
  chat_id    INTEGER NOT NULL REFERENCES chats (id) ON UPDATE CASCADE,
  num        INTEGER NOT NULL,
  text       TEXT    NOT NULL,
  status     TEXT    NOT NULL DEFAULT 'open' CHECK (status IN ('open','done','deleted')),
  due_at     INTEGER,
  recur      TEXT,                              -- canonical recur.Rule string
  tz         TEXT    NOT NULL,                  -- creator's TZ at creation / last SetTZ
  created_by INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  done_at    INTEGER,
  UNIQUE (chat_id, num)
);
CREATE INDEX todos_chat_status_due ON todos (chat_id, status, due_at);
CREATE INDEX todos_recurring_creator ON todos (created_by) WHERE recur IS NOT NULL AND status = 'open';

CREATE TABLE reminders (
  id         INTEGER PRIMARY KEY,
  todo_id    INTEGER NOT NULL REFERENCES todos (id) ON DELETE CASCADE,
  chat_id    INTEGER NOT NULL,
  fire_at    INTEGER NOT NULL,
  kind       TEXT    NOT NULL CHECK (kind IN ('due','custom','snooze')),
  state      TEXT    NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','sent','cancelled','failed')),
  attempts   INTEGER NOT NULL DEFAULT 0,
  sent_at    INTEGER,
  message_id INTEGER,
  created_at INTEGER NOT NULL
);
CREATE INDEX reminders_pending ON reminders (fire_at) WHERE state = 'pending';
CREATE UNIQUE INDEX reminders_one_pending_due ON reminders (todo_id) WHERE state = 'pending' AND kind = 'due';
