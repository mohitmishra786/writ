-- Prompt-cache accounting for providers that report it (Claude).
ALTER TABLE ai_calls ADD COLUMN cache_read_tokens int NOT NULL DEFAULT 0;
ALTER TABLE ai_calls ADD COLUMN cache_write_tokens int NOT NULL DEFAULT 0;
