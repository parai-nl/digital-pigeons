-- Chat-sessies en -berichten van de duifmodule.
-- Chatdata, geen vergaderdata: de regel dat een vergadering niets opslaat gaat
-- uitsluitend over vergaderaudio en -transcript en blijft onverkort gelden.

CREATE TABLE duif.chat_sessions (
    id text PRIMARY KEY,
    user_id text NOT NULL,
    pigeon_id text NOT NULL,
    lang text NOT NULL DEFAULT 'en' CHECK (lang IN ('en', 'nl')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX chat_sessions_user_updated_idx
    ON duif.chat_sessions (user_id, updated_at DESC, id DESC);

CREATE TABLE duif.chat_messages (
    id text PRIMARY KEY,
    session_id text NOT NULL REFERENCES duif.chat_sessions(id) ON DELETE CASCADE,
    role text NOT NULL CHECK (role IN ('user', 'assistant')),
    content text NOT NULL,
    kb_status text NOT NULL DEFAULT '',
    sources jsonb NOT NULL DEFAULT '[]'::jsonb,
    partial boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX chat_messages_session_created_idx
    ON duif.chat_messages (session_id, created_at ASC, id ASC);
