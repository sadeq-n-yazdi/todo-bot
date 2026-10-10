-- Assigning a todo to another user (#58, ARCHITECTURE section 11).

-- Telegram @username, lowercase and without "@"; NULL = unknown. A name is
-- unique among users: store.SetUsername clears it from the previous owner.
ALTER TABLE users ADD COLUMN username TEXT;
CREATE UNIQUE INDEX users_username ON users (username) WHERE username IS NOT NULL;

-- The user the item is assigned to; NULL = none. Its reminders go to that
-- user's private chat (chat id = user id); the item stays in chat_id.
ALTER TABLE todos ADD COLUMN assignee_id INTEGER REFERENCES users (id);
CREATE INDEX todos_assignee_status ON todos (assignee_id, status) WHERE assignee_id IS NOT NULL;
