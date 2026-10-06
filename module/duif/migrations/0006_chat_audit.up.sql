-- Additive only: the semantic audit was a label written after an answer was
-- already delivered, never a gate in front of it. Live turns no longer write
-- it; the column stays for rows that carry one. Existing rows keep NULL, which
-- reads as "no audit applies", never as "this answer failed a check".
ALTER TABLE duif.chat_messages ADD COLUMN audit jsonb;
