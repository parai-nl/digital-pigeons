-- Additive only: legacy answers retain NULL provenance and their exact content.
ALTER TABLE duif.chat_messages ADD COLUMN provenance jsonb;
ALTER TABLE duif.chat_sessions
    ADD COLUMN comparison jsonb,
    ADD COLUMN origin_session_id text REFERENCES duif.chat_sessions(id) ON DELETE SET NULL;

-- When a source session is deleted, remove its private comparison references.
-- Explicitly copied user context in the target belongs to that target session.
CREATE FUNCTION duif.clear_deleted_comparison_origin() RETURNS trigger AS $$
BEGIN
    IF OLD.origin_session_id IS NOT NULL AND NEW.origin_session_id IS NULL THEN
        NEW.comparison := NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER clear_deleted_comparison_origin
    BEFORE UPDATE OF origin_session_id ON duif.chat_sessions
    FOR EACH ROW EXECUTE FUNCTION duif.clear_deleted_comparison_origin();
