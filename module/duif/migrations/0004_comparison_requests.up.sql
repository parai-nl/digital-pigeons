-- Nullable for ordinary and historical sessions; one target per user intent.
ALTER TABLE duif.chat_sessions
 ADD COLUMN comparison_request_id text,
 ADD COLUMN comparison_request_hash text;
CREATE UNIQUE INDEX chat_comparison_request_unique
 ON duif.chat_sessions(user_id,comparison_request_id)
 WHERE comparison_request_id IS NOT NULL;
