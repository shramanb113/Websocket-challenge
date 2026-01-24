ALTER TABLE messages 
ADD COLUMN status SMALLINT NOT NULL DEFAULT 0;

CREATE INDEX idx_messages_unread 
ON messages (room_id, target_user, status) 
WHERE status < 2;