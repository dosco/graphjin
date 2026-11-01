-- GJ4 — opt-in source/agentic mode per data source. 'none' = today's
-- anonymous legacy mode; 'jwt' = GraphJin runs auth: jwt and every run's
-- CLI/MCP calls carry a short-lived actor token (K1 snapshot), so the
-- engine itself shapes what each caller sees.

ALTER TABLE data_source
  ADD COLUMN IF NOT EXISTS auth_mode text NOT NULL DEFAULT 'none';
