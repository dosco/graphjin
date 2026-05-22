package codesql

import (
	"context"
	"database/sql"
)

const schemaSQL = `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=30000;

CREATE TABLE IF NOT EXISTS code_languages (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  extensions TEXT NOT NULL,
  parser TEXT NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS code_grammars (
  id INTEGER PRIMARY KEY,
  language_id INTEGER NOT NULL REFERENCES code_languages(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  version TEXT NOT NULL,
  abi_version INTEGER NOT NULL,
  UNIQUE(language_id, name)
);

CREATE TABLE IF NOT EXISTS code_query_packs (
  id INTEGER PRIMARY KEY,
  language_id INTEGER NOT NULL REFERENCES code_languages(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  hash TEXT NOT NULL,
  source TEXT NOT NULL,
  UNIQUE(language_id, kind)
);

CREATE TABLE IF NOT EXISTS code_files (
  id INTEGER PRIMARY KEY,
  path TEXT NOT NULL UNIQUE,
  abs_path TEXT NOT NULL,
  language TEXT NOT NULL,
  hash TEXT NOT NULL,
  size INTEGER NOT NULL,
  mtime_unix INTEGER NOT NULL,
  indexed_at TEXT NOT NULL,
  is_virtual BOOLEAN NOT NULL DEFAULT 0,
  parent_file_id INTEGER REFERENCES code_files(id) ON DELETE CASCADE,
  start_byte INTEGER NOT NULL DEFAULT 0,
  end_byte INTEGER NOT NULL DEFAULT 0,
  start_row INTEGER NOT NULL DEFAULT 0,
  start_col INTEGER NOT NULL DEFAULT 0,
  end_row INTEGER NOT NULL DEFAULT 0,
  end_col INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS code_file_versions (
  id INTEGER PRIMARY KEY,
  file_id INTEGER NOT NULL REFERENCES code_files(id) ON DELETE CASCADE,
  hash TEXT NOT NULL,
  size INTEGER NOT NULL,
  mtime_unix INTEGER NOT NULL,
  grammar_hash TEXT NOT NULL,
  query_pack_hash TEXT NOT NULL,
  indexed_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS code_index_status (
  id INTEGER PRIMARY KEY,
  root TEXT NOT NULL,
  cache_path TEXT NOT NULL,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  files_seen INTEGER NOT NULL DEFAULT 0,
  files_added INTEGER NOT NULL DEFAULT 0,
  files_changed INTEGER NOT NULL DEFAULT 0,
  files_deleted INTEGER NOT NULL DEFAULT 0,
  files_skipped INTEGER NOT NULL DEFAULT 0,
  parse_errors INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS code_parse_errors (
  id INTEGER PRIMARY KEY,
  file_id INTEGER REFERENCES code_files(id) ON DELETE CASCADE,
  path TEXT NOT NULL,
  language TEXT NOT NULL,
  message TEXT NOT NULL,
  start_row INTEGER NOT NULL DEFAULT 0,
  start_col INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS code_nodes (
  id INTEGER PRIMARY KEY,
  file_id INTEGER NOT NULL REFERENCES code_files(id) ON DELETE CASCADE,
  parent_node_id INTEGER REFERENCES code_nodes(id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL,
  field_name TEXT NOT NULL DEFAULT '',
  node_type TEXT NOT NULL,
  grammar_name TEXT NOT NULL DEFAULT '',
  is_named BOOLEAN NOT NULL,
  is_extra BOOLEAN NOT NULL,
  has_error BOOLEAN NOT NULL,
  is_error BOOLEAN NOT NULL,
  is_missing BOOLEAN NOT NULL,
  start_byte INTEGER NOT NULL,
  end_byte INTEGER NOT NULL,
  start_row INTEGER NOT NULL,
  start_col INTEGER NOT NULL,
  end_row INTEGER NOT NULL,
  end_col INTEGER NOT NULL,
  depth INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_code_nodes_file ON code_nodes(file_id);
CREATE INDEX IF NOT EXISTS idx_code_nodes_type ON code_nodes(node_type);

CREATE TABLE IF NOT EXISTS code_captures (
  id INTEGER PRIMARY KEY,
  file_id INTEGER NOT NULL REFERENCES code_files(id) ON DELETE CASCADE,
  node_id INTEGER REFERENCES code_nodes(id) ON DELETE CASCADE,
  query_kind TEXT NOT NULL,
  capture_name TEXT NOT NULL,
  pattern_index INTEGER NOT NULL,
  text TEXT NOT NULL,
  start_byte INTEGER NOT NULL,
  end_byte INTEGER NOT NULL,
  start_row INTEGER NOT NULL,
  start_col INTEGER NOT NULL,
  end_row INTEGER NOT NULL,
  end_col INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_code_captures_file ON code_captures(file_id);
CREATE INDEX IF NOT EXISTS idx_code_captures_name ON code_captures(capture_name);

CREATE TABLE IF NOT EXISTS code_symbols (
  id INTEGER PRIMARY KEY,
  file_id INTEGER NOT NULL REFERENCES code_files(id) ON DELETE CASCADE,
  parent_symbol_id INTEGER REFERENCES code_symbols(id) ON DELETE SET NULL,
  language TEXT NOT NULL,
  kind TEXT NOT NULL,
  name TEXT NOT NULL,
  qualified_name TEXT NOT NULL DEFAULT '',
  signature TEXT NOT NULL DEFAULT '',
  doc TEXT NOT NULL DEFAULT '',
  node_id INTEGER REFERENCES code_nodes(id) ON DELETE SET NULL,
  start_byte INTEGER NOT NULL,
  end_byte INTEGER NOT NULL,
  start_row INTEGER NOT NULL,
  start_col INTEGER NOT NULL,
  end_row INTEGER NOT NULL,
  end_col INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_code_symbols_file ON code_symbols(file_id);
CREATE INDEX IF NOT EXISTS idx_code_symbols_name ON code_symbols(name);
CREATE INDEX IF NOT EXISTS idx_code_symbols_kind ON code_symbols(kind);

CREATE VIRTUAL TABLE IF NOT EXISTS code_symbols_fts USING fts5(
  name, qualified_name, signature, doc,
  content='code_symbols', content_rowid='id'
);

CREATE TRIGGER IF NOT EXISTS code_symbols_ai AFTER INSERT ON code_symbols BEGIN
  INSERT INTO code_symbols_fts(rowid, name, qualified_name, signature, doc)
  VALUES (new.id, new.name, new.qualified_name, new.signature, new.doc);
END;
CREATE TRIGGER IF NOT EXISTS code_symbols_ad AFTER DELETE ON code_symbols BEGIN
  INSERT INTO code_symbols_fts(code_symbols_fts, rowid, name, qualified_name, signature, doc)
  VALUES ('delete', old.id, old.name, old.qualified_name, old.signature, old.doc);
END;
CREATE TRIGGER IF NOT EXISTS code_symbols_au AFTER UPDATE ON code_symbols BEGIN
  INSERT INTO code_symbols_fts(code_symbols_fts, rowid, name, qualified_name, signature, doc)
  VALUES ('delete', old.id, old.name, old.qualified_name, old.signature, old.doc);
  INSERT INTO code_symbols_fts(rowid, name, qualified_name, signature, doc)
  VALUES (new.id, new.name, new.qualified_name, new.signature, new.doc);
END;

CREATE TABLE IF NOT EXISTS code_scopes (
  id INTEGER PRIMARY KEY,
  file_id INTEGER NOT NULL REFERENCES code_files(id) ON DELETE CASCADE,
  parent_scope_id INTEGER REFERENCES code_scopes(id) ON DELETE SET NULL,
  kind TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  node_id INTEGER REFERENCES code_nodes(id) ON DELETE SET NULL,
  start_byte INTEGER NOT NULL,
  end_byte INTEGER NOT NULL,
  start_row INTEGER NOT NULL,
  start_col INTEGER NOT NULL,
  end_row INTEGER NOT NULL,
  end_col INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS code_locals (
  id INTEGER PRIMARY KEY,
  file_id INTEGER NOT NULL REFERENCES code_files(id) ON DELETE CASCADE,
  scope_id INTEGER REFERENCES code_scopes(id) ON DELETE SET NULL,
  name TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT '',
  node_id INTEGER REFERENCES code_nodes(id) ON DELETE SET NULL,
  start_byte INTEGER NOT NULL,
  end_byte INTEGER NOT NULL,
  start_row INTEGER NOT NULL,
  start_col INTEGER NOT NULL,
  end_row INTEGER NOT NULL,
  end_col INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS code_refs (
  id INTEGER PRIMARY KEY,
  file_id INTEGER NOT NULL REFERENCES code_files(id) ON DELETE CASCADE,
  symbol_id INTEGER REFERENCES code_symbols(id) ON DELETE SET NULL,
  name TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT '',
  node_id INTEGER REFERENCES code_nodes(id) ON DELETE SET NULL,
  start_byte INTEGER NOT NULL,
  end_byte INTEGER NOT NULL,
  start_row INTEGER NOT NULL,
  start_col INTEGER NOT NULL,
  end_row INTEGER NOT NULL,
  end_col INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_code_refs_file ON code_refs(file_id);
CREATE INDEX IF NOT EXISTS idx_code_refs_name ON code_refs(name);

CREATE TABLE IF NOT EXISTS code_imports (
  id INTEGER PRIMARY KEY,
  file_id INTEGER NOT NULL REFERENCES code_files(id) ON DELETE CASCADE,
  language TEXT NOT NULL,
  path TEXT NOT NULL,
  alias TEXT NOT NULL DEFAULT '',
  node_id INTEGER REFERENCES code_nodes(id) ON DELETE SET NULL,
  start_byte INTEGER NOT NULL,
  end_byte INTEGER NOT NULL,
  start_row INTEGER NOT NULL,
  start_col INTEGER NOT NULL,
  end_row INTEGER NOT NULL,
  end_col INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_code_imports_file ON code_imports(file_id);
CREATE INDEX IF NOT EXISTS idx_code_imports_path ON code_imports(path);

CREATE TABLE IF NOT EXISTS code_edges (
  id INTEGER PRIMARY KEY,
  file_id INTEGER NOT NULL REFERENCES code_files(id) ON DELETE CASCADE,
  source_symbol_id INTEGER REFERENCES code_symbols(id) ON DELETE SET NULL,
  target_symbol_id INTEGER REFERENCES code_symbols(id) ON DELETE SET NULL,
  ref_id INTEGER REFERENCES code_refs(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  confidence TEXT NOT NULL DEFAULT 'syntax'
);

CREATE TABLE IF NOT EXISTS code_db_refs (
  id INTEGER PRIMARY KEY,
  file_id INTEGER NOT NULL REFERENCES code_files(id) ON DELETE CASCADE,
  symbol_id INTEGER REFERENCES code_symbols(id) ON DELETE SET NULL,
  node_id INTEGER REFERENCES code_nodes(id) ON DELETE SET NULL,
  database_name TEXT NOT NULL DEFAULT '',
  schema_name TEXT NOT NULL DEFAULT '',
  table_name TEXT NOT NULL DEFAULT '',
  column_name TEXT NOT NULL DEFAULT '',
  table_key TEXT NOT NULL DEFAULT '',
  column_key TEXT NOT NULL DEFAULT '',
  ref_kind TEXT NOT NULL,
  confidence REAL NOT NULL DEFAULT 0,
  evidence TEXT NOT NULL DEFAULT '',
  source_text TEXT NOT NULL DEFAULT '',
  start_byte INTEGER NOT NULL DEFAULT 0,
  end_byte INTEGER NOT NULL DEFAULT 0,
  start_row INTEGER NOT NULL DEFAULT 0,
  start_col INTEGER NOT NULL DEFAULT 0,
  end_row INTEGER NOT NULL DEFAULT 0,
  end_col INTEGER NOT NULL DEFAULT 0,
  resolved BOOLEAN NOT NULL DEFAULT 0,
  ambiguous BOOLEAN NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_code_db_refs_table_key ON code_db_refs(table_key);
CREATE INDEX IF NOT EXISTS idx_code_db_refs_column_key ON code_db_refs(column_key);
CREATE INDEX IF NOT EXISTS idx_code_db_refs_table ON code_db_refs(table_name);
CREATE INDEX IF NOT EXISTS idx_code_db_refs_column ON code_db_refs(column_name);
CREATE INDEX IF NOT EXISTS idx_code_db_refs_file ON code_db_refs(file_id);
CREATE INDEX IF NOT EXISTS idx_code_db_refs_symbol ON code_db_refs(symbol_id);

CREATE TABLE IF NOT EXISTS code_injections (
  id INTEGER PRIMARY KEY,
  file_id INTEGER NOT NULL REFERENCES code_files(id) ON DELETE CASCADE,
  virtual_file_id INTEGER REFERENCES code_files(id) ON DELETE CASCADE,
  language TEXT NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  node_id INTEGER REFERENCES code_nodes(id) ON DELETE SET NULL,
  start_byte INTEGER NOT NULL,
  end_byte INTEGER NOT NULL,
  start_row INTEGER NOT NULL,
  start_col INTEGER NOT NULL,
  end_row INTEGER NOT NULL,
  end_col INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS code_docs (
  id INTEGER PRIMARY KEY,
  file_id INTEGER NOT NULL REFERENCES code_files(id) ON DELETE CASCADE,
  symbol_id INTEGER REFERENCES code_symbols(id) ON DELETE SET NULL,
  text TEXT NOT NULL,
  node_id INTEGER REFERENCES code_nodes(id) ON DELETE SET NULL,
  start_row INTEGER NOT NULL DEFAULT 0,
  start_col INTEGER NOT NULL DEFAULT 0
);

CREATE VIRTUAL TABLE IF NOT EXISTS code_docs_fts USING fts5(
  text,
  content='code_docs', content_rowid='id'
);

CREATE TRIGGER IF NOT EXISTS code_docs_ai AFTER INSERT ON code_docs BEGIN
  INSERT INTO code_docs_fts(rowid, text) VALUES (new.id, new.text);
END;
CREATE TRIGGER IF NOT EXISTS code_docs_ad AFTER DELETE ON code_docs BEGIN
  INSERT INTO code_docs_fts(code_docs_fts, rowid, text) VALUES ('delete', old.id, old.text);
END;
CREATE TRIGGER IF NOT EXISTS code_docs_au AFTER UPDATE ON code_docs BEGIN
  INSERT INTO code_docs_fts(code_docs_fts, rowid, text) VALUES ('delete', old.id, old.text);
  INSERT INTO code_docs_fts(rowid, text) VALUES (new.id, new.text);
END;

CREATE TABLE IF NOT EXISTS code_file_text_chunks (
  id INTEGER PRIMARY KEY,
  file_id INTEGER NOT NULL REFERENCES code_files(id) ON DELETE CASCADE,
  chunk_index INTEGER NOT NULL,
  text TEXT NOT NULL,
  start_byte INTEGER NOT NULL,
  end_byte INTEGER NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS code_file_text_chunks_fts USING fts5(
  text,
  content='code_file_text_chunks', content_rowid='id'
);

CREATE TRIGGER IF NOT EXISTS code_file_text_chunks_ai AFTER INSERT ON code_file_text_chunks BEGIN
  INSERT INTO code_file_text_chunks_fts(rowid, text) VALUES (new.id, new.text);
END;
CREATE TRIGGER IF NOT EXISTS code_file_text_chunks_ad AFTER DELETE ON code_file_text_chunks BEGIN
  INSERT INTO code_file_text_chunks_fts(code_file_text_chunks_fts, rowid, text) VALUES ('delete', old.id, old.text);
END;
CREATE TRIGGER IF NOT EXISTS code_file_text_chunks_au AFTER UPDATE ON code_file_text_chunks BEGIN
  INSERT INTO code_file_text_chunks_fts(code_file_text_chunks_fts, rowid, text) VALUES ('delete', old.id, old.text);
  INSERT INTO code_file_text_chunks_fts(rowid, text) VALUES (new.id, new.text);
END;

CREATE TABLE IF NOT EXISTS code_change_sets (
  id INTEGER PRIMARY KEY,
  action TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  owner TEXT NOT NULL DEFAULT '',
  edits JSON NOT NULL DEFAULT '[]',
  lock_tokens JSON NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'pending',
  diff TEXT NOT NULL DEFAULT '',
  errors JSON NOT NULL DEFAULT '[]',
  files_changed JSON NOT NULL DEFAULT '[]',
  files_reindexed JSON NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  applied_at TEXT
);

CREATE TABLE IF NOT EXISTS code_locks (
  id INTEGER PRIMARY KEY,
  action TEXT NOT NULL DEFAULT '',
  path TEXT NOT NULL,
  ranges JSON NOT NULL DEFAULT '[]',
  owner TEXT NOT NULL DEFAULT '',
  lease_token TEXT NOT NULL DEFAULT '',
  ttl_seconds INTEGER NOT NULL DEFAULT 120,
  whole_file BOOLEAN NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'active',
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_code_locks_path_status ON code_locks(path, status);
CREATE INDEX IF NOT EXISTS idx_code_locks_expires_at ON code_locks(expires_at);

CREATE TABLE IF NOT EXISTS code_change_audit (
  id INTEGER PRIMARY KEY,
  change_set_id INTEGER REFERENCES code_change_sets(id) ON DELETE SET NULL,
  lock_id INTEGER REFERENCES code_locks(id) ON DELETE SET NULL,
  event TEXT NOT NULL,
  path TEXT NOT NULL DEFAULT '',
  message TEXT NOT NULL DEFAULT '',
  details JSON NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS gj_code (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  path TEXT NOT NULL DEFAULT '',
  abs_path TEXT NOT NULL DEFAULT '',
  language TEXT NOT NULL DEFAULT '',
  hash TEXT NOT NULL DEFAULT '',
  symbol_kind TEXT NOT NULL DEFAULT '',
  qualified_name TEXT NOT NULL DEFAULT '',
  signature TEXT NOT NULL DEFAULT '',
  doc TEXT NOT NULL DEFAULT '',
  db_object_id TEXT NOT NULL DEFAULT '',
  database_name TEXT NOT NULL DEFAULT '',
  schema_name TEXT NOT NULL DEFAULT '',
  table_name TEXT NOT NULL DEFAULT '',
  column_name TEXT NOT NULL DEFAULT '',
  catalog_item_id TEXT NOT NULL DEFAULT '',
  table_catalog_item_id TEXT NOT NULL DEFAULT '',
  column_catalog_item_id TEXT NOT NULL DEFAULT '',
  file_id TEXT,
  symbol_id TEXT,
  parent_id TEXT,
  target_symbol_id TEXT,
  source_table TEXT NOT NULL DEFAULT '',
  source_id INTEGER NOT NULL DEFAULT 0,
  node_type TEXT NOT NULL DEFAULT '',
  edge_kind TEXT NOT NULL DEFAULT '',
  ref_kind TEXT NOT NULL DEFAULT '',
  import_path TEXT NOT NULL DEFAULT '',
  alias TEXT NOT NULL DEFAULT '',
  start_byte INTEGER NOT NULL DEFAULT 0,
  end_byte INTEGER NOT NULL DEFAULT 0,
  start_row INTEGER NOT NULL DEFAULT 0,
  start_col INTEGER NOT NULL DEFAULT 0,
  end_row INTEGER NOT NULL DEFAULT 0,
  end_col INTEGER NOT NULL DEFAULT 0,
  action TEXT NOT NULL DEFAULT '',
  owner TEXT NOT NULL DEFAULT '',
  edits JSON NOT NULL DEFAULT '[]',
  lock_tokens JSON NOT NULL DEFAULT '[]',
  ranges JSON NOT NULL DEFAULT '[]',
  lease_token TEXT NOT NULL DEFAULT '',
  ttl_seconds INTEGER NOT NULL DEFAULT 0,
  whole_file BOOLEAN NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT '',
  diff TEXT NOT NULL DEFAULT '',
  errors_json JSON NOT NULL DEFAULT '[]',
  files_changed JSON NOT NULL DEFAULT '[]',
  files_reindexed JSON NOT NULL DEFAULT '[]',
  details_json JSON NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT '',
  search_rank REAL NOT NULL DEFAULT 0,
  search_vector TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_gj_code_kind ON gj_code(kind);
CREATE INDEX IF NOT EXISTS idx_gj_code_path ON gj_code(path);
CREATE INDEX IF NOT EXISTS idx_gj_code_db_object ON gj_code(db_object_id);
CREATE INDEX IF NOT EXISTS idx_gj_code_catalog_item ON gj_code(catalog_item_id);
CREATE INDEX IF NOT EXISTS idx_gj_code_file ON gj_code(file_id);
CREATE INDEX IF NOT EXISTS idx_gj_code_symbol ON gj_code(symbol_id);
CREATE INDEX IF NOT EXISTS idx_gj_code_parent ON gj_code(parent_id);

CREATE VIRTUAL TABLE IF NOT EXISTS gj_code_fts USING fts5(
  name, title, summary, path, qualified_name, signature, doc, search_vector,
  content='gj_code', content_rowid='rowid'
);

CREATE TRIGGER IF NOT EXISTS gj_code_ai AFTER INSERT ON gj_code BEGIN
  INSERT INTO gj_code_fts(rowid, name, title, summary, path, qualified_name, signature, doc, search_vector)
  VALUES (new.rowid, new.name, new.title, new.summary, new.path, new.qualified_name, new.signature, new.doc, new.search_vector);
END;
CREATE TRIGGER IF NOT EXISTS gj_code_ad AFTER DELETE ON gj_code BEGIN
  INSERT INTO gj_code_fts(gj_code_fts, rowid, name, title, summary, path, qualified_name, signature, doc, search_vector)
  VALUES ('delete', old.rowid, old.name, old.title, old.summary, old.path, old.qualified_name, old.signature, old.doc, old.search_vector);
END;
CREATE TRIGGER IF NOT EXISTS gj_code_au AFTER UPDATE ON gj_code BEGIN
  INSERT INTO gj_code_fts(gj_code_fts, rowid, name, title, summary, path, qualified_name, signature, doc, search_vector)
  VALUES ('delete', old.rowid, old.name, old.title, old.summary, old.path, old.qualified_name, old.signature, old.doc, old.search_vector);
  INSERT INTO gj_code_fts(rowid, name, title, summary, path, qualified_name, signature, doc, search_vector)
  VALUES (new.rowid, new.name, new.title, new.summary, new.path, new.qualified_name, new.signature, new.doc, new.search_vector);
END;
`

func ensureSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, schemaSQL)
	return err
}
