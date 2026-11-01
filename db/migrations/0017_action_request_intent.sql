-- Adds a nullable `intent` column to action_request — the agent's
-- short natural-language framing of what it intends to do when it
-- requests an ask-mode action. Surfaced as the headline on the
-- inline approval card in /work so users approve the agent's
-- *stated* purpose, not a raw payload.
--
-- Required at the schema-shape level for ask-mode requests, but
-- enforced at the application layer (the tool builder injects
-- `intent` as a required tool arg) so retrofitting historical rows
-- doesn't break the migration. NULL is permitted for older rows
-- and for auto/internal action_requests where no human ever sees
-- the framing.
ALTER TABLE action_request
ADD COLUMN intent text;
