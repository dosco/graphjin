package codesql

import (
	"context"
	"database/sql"
)

const schemaSQL = `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;

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
`

func ensureSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, schemaSQL)
	return err
}
