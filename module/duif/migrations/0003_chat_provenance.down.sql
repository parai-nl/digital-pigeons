DROP TRIGGER IF EXISTS clear_deleted_comparison_origin ON duif.chat_sessions;
DROP FUNCTION IF EXISTS duif.clear_deleted_comparison_origin();
ALTER TABLE duif.chat_sessions DROP COLUMN origin_session_id, DROP COLUMN comparison;
ALTER TABLE duif.chat_messages DROP COLUMN provenance;
