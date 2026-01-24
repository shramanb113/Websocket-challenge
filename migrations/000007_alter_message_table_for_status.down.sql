DROP INDEX IF EXISTS idx_messages_unread;

ALTER TABLE messages 
DROP COLUMN IF EXISTS status;