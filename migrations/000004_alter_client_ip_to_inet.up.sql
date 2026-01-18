
ALTER TABLE refresh_tokens 
    ALTER COLUMN client_ip TYPE INET 
    USING client_ip::inet;

-- Optional: Add a comment to explain the type choice
COMMENT ON COLUMN refresh_tokens.client_ip IS 'Supports both IPv4 and IPv6 addresses';