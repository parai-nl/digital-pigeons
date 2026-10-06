-- Additive only: a comparison copies an answer into a second conversation, and
-- the coverage label of that answer can still be on its way at that moment
-- (the audit ran after the text was delivered). This column names the
-- answer a row was copied from, so the label that lands seconds later reaches
-- the copy too instead of freezing it without one.
--
-- It always names the ORIGINAL answer, never an intermediate copy, so a copy of
-- a copy is reached by the same single write. Existing rows keep '', which
-- reads as "this row is not a copy of anything".
ALTER TABLE duif.chat_messages ADD COLUMN source_message_id text NOT NULL DEFAULT '';
-- Bewust zonder voorwaarde. De enige lezer is
-- `WHERE id=$1 OR source_message_id=$1`, en een gebonden parameter is voor de
-- planner onbekend: onder een generiek plan kan hij `source_message_id <> ''`
-- niet bewijzen, laat een partiële index links liggen en scant de hele tabel.
-- De lege waarden die we daarmee meenemen zijn goedkoop — ze staan in één
-- B-tree-tak — en dat is de prijs voor een index die ook werkt wanneer hij
-- nodig is.
CREATE INDEX chat_messages_source_message ON duif.chat_messages(source_message_id);
