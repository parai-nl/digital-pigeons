-- Additive only: `related` says these sources came from the broader search
-- that runs after the normal source threshold finds nothing, so the
-- reader sees on reload what they saw live — that the sources border on the
-- question instead of answering it head-on. Existing rows keep false, which
-- reads as "this came from the ordinary search": true for every row written
-- before the broader search existed.
ALTER TABLE duif.chat_messages ADD COLUMN related boolean NOT NULL DEFAULT false;
