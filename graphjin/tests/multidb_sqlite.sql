-- SQLite schema for multi-DB tests
-- Contains: audit_logs, local_cache (edge/local data)

CREATE TABLE audit_logs (
  id INTEGER PRIMARY KEY,
  action TEXT NOT NULL,
  entity_type TEXT NOT NULL,
  entity_id INTEGER NOT NULL,
  user_id INTEGER,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE local_cache (
  id INTEGER PRIMARY KEY,
  cache_key TEXT UNIQUE NOT NULL,
  cache_value TEXT,
  expires_at TIMESTAMP
);

-- Insert test data (references users in postgres via user_id)
INSERT INTO audit_logs (id, action, entity_type, entity_id, user_id) VALUES
  (1, 'CREATE', 'product', 1, 1),
  (2, 'UPDATE', 'product', 1, 1),
  (3, 'CREATE', 'product', 2, 2);

INSERT INTO local_cache (id, cache_key, cache_value) VALUES
  (1, 'settings:user:1', '{"theme": "dark"}'),
  (2, 'settings:user:2', '{"theme": "light"}');
