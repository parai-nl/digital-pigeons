DROP INDEX duif.chat_comparison_request_unique;
ALTER TABLE duif.chat_sessions DROP COLUMN comparison_request_id,DROP COLUMN comparison_request_hash;
