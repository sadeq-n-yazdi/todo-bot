-- Done on a reminder message (#157). acked_at is the Unix time (UTC) of the
-- first Done press on a sent reminder, 0 if none. store.MarkReminderAcked
-- sets it only while it is 0, so of several presses exactly one wins.
ALTER TABLE reminders ADD COLUMN acked_at INTEGER NOT NULL DEFAULT 0;
