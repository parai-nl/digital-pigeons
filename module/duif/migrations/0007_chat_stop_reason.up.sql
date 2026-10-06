-- Additive only: a turn that the provider closed on the token ceiling stops
-- halfway through a sentence, and until now said nothing about it. The
-- column records that, never the text: existing rows keep '' and read as the
-- normal case, which is exactly what they were.
ALTER TABLE duif.chat_messages ADD COLUMN stop_reason text NOT NULL DEFAULT '';
