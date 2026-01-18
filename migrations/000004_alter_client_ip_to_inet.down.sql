-- Revert INET back to VARCHAR(45)
ALTER TABLE refresh_tokens 
    ALTER COLUMN client_ip TYPE VARCHAR(45) 
    USING client_ip::text;

COMMENT ON COLUMN refresh_tokens.client_ip IS NULL;