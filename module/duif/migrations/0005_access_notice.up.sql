CREATE TABLE duif.access_notice (
    user_id text PRIMARY KEY,
    version text NOT NULL,
    acknowledged_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE duif.access_notice IS 'Latest product explanation acknowledgement; no conversation data or legal consent assertion.';
