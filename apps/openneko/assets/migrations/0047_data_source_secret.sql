-- OL5 — named connection secrets for chat-first source config. The admin
-- stores a value under a NAME (settings form / CLI); the agent only ever
-- references the name; the worker resolves + decrypts it at apply time and
-- injects it into the GraphJin config. Values are enc:v1 at rest (SEC1).

CREATE TABLE IF NOT EXISTS data_source_secret (
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  name text NOT NULL,
  value_enc text NOT NULL,
  description text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, name)
);
