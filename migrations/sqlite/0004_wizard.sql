-- Step-by-step task creation wizard (#68, ARCHITECTURE section 12).
-- Times are INTEGER Unix seconds (UTC).

-- One draft per (chat, user); starting the wizard again replaces it.
-- Keys and coordination fields are columns; the answers collected so far
-- are a JSON object (model.DraftData) in data, so a later step can add a
-- field without a migration. Nothing queries inside data.
--
-- version is the compare-and-set token: every write is conditional on the
-- version the writer read, and bumps it, so two nodes handling two presses
-- of the same card cannot both apply. Callback data carries it, so a press
-- on an out-of-date card is detected.
--
-- No foreign keys: a group's chats row may not exist yet (EnsureChat runs
-- on the first item), and MigrateChat must not be blocked by drafts. An
-- abandoned draft is removed by Purge once expires_at has passed.
CREATE TABLE drafts (
  chat_id    INTEGER NOT NULL,
  user_id    INTEGER NOT NULL,
  msg_id     INTEGER NOT NULL DEFAULT 0, -- the wizard card (inline keyboard)
  ask_msg_id INTEGER NOT NULL DEFAULT 0, -- a ForceReply question, 0 = none
  step       TEXT    NOT NULL,
  data       TEXT    NOT NULL DEFAULT '{}',
  version    INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  PRIMARY KEY (chat_id, user_id)
);
-- Button presses find the draft by the card they were pressed on.
CREATE INDEX drafts_msg ON drafts (chat_id, msg_id);
CREATE INDEX drafts_expires ON drafts (expires_at);
