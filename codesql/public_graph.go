package codesql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const gjCodeColumns = `id, kind, name, title, summary, path, abs_path, language, hash, symbol_kind, qualified_name, signature, doc,
db_object_id, database_name, schema_name, table_name, column_name, catalog_item_id, table_catalog_item_id, column_catalog_item_id,
file_id, symbol_id, parent_id, target_symbol_id, source_table, source_id, node_type, edge_kind, ref_kind, import_path, alias,
start_byte, end_byte, start_row, start_col, end_row, end_col, action, owner, edits, lock_tokens, ranges, lease_token,
ttl_seconds, whole_file, status, diff, errors_json, files_changed, files_reindexed, details_json, created_at, updated_at, search_vector`

func RefreshPublicGraph(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM gj_code`); err != nil {
		tx.Rollback() //nolint:errcheck
		return err
	}
	for i, stmt := range gjCodeRefreshStatements() {
		if _, err = tx.ExecContext(ctx, stmt); err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("refresh gj_code statement %d: %s: %w", i+1, shortSQL(stmt), err)
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO gj_code_fts(gj_code_fts) VALUES ('rebuild')`); err != nil {
		tx.Rollback() //nolint:errcheck
		return err
	}
	return tx.Commit()
}

func shortSQL(stmt string) string {
	const max = 80
	stmt = strings.Join(strings.Fields(stmt), " ")
	if len(stmt) <= max {
		return stmt
	}
	return stmt[:max] + "..."
}

func gjCodeRefreshStatements() []string {
	return []string{
		`INSERT INTO gj_code (` + gjCodeColumns + `)
SELECT 'file:' || f.id, 'file', f.path, f.path, 'Source file ' || f.path, f.path, f.abs_path, f.language, f.hash, '', '', '', '',
'', '', '', '', '', '', '', '',
'', '', CASE WHEN f.parent_file_id IS NULL THEN '' ELSE 'file:' || f.parent_file_id END, '', 'code_files', f.id, '', '', '', '', '',
f.start_byte, f.end_byte, f.start_row, f.start_col, f.end_row, f.end_col, '', '', '[]', '[]', '[]', '', 0, 0, '', '', '[]', '[]', '[]', '{}', f.indexed_at, f.indexed_at,
f.path || ' ' || f.language
FROM code_files f`,

		`INSERT INTO gj_code (` + gjCodeColumns + `)
SELECT 'symbol:' || s.id, 'symbol', s.name, s.qualified_name, COALESCE(NULLIF(s.signature, ''), s.name), f.path, f.abs_path, s.language, f.hash, s.kind, s.qualified_name, s.signature, s.doc,
'', '', '', '', '', '', '', '',
'file:' || s.file_id, '', CASE WHEN s.parent_symbol_id IS NULL THEN '' ELSE 'symbol:' || s.parent_symbol_id END, '', 'code_symbols', s.id, '', '', '', '', '',
s.start_byte, s.end_byte, s.start_row, s.start_col, s.end_row, s.end_col, '', '', '[]', '[]', '[]', '', 0, 0, '', '', '[]', '[]', '[]', '{}', '', '',
s.name || ' ' || s.qualified_name || ' ' || s.signature || ' ' || s.doc || ' ' || f.path
FROM code_symbols s JOIN code_files f ON f.id = s.file_id`,

		`INSERT INTO gj_code (` + gjCodeColumns + `)
SELECT 'reference:' || r.id, 'reference', r.name, r.name, r.kind, f.path, f.abs_path, f.language, f.hash, '', '', '', '',
'', '', '', '', '', '', '', '',
'file:' || r.file_id, CASE WHEN r.symbol_id IS NULL THEN '' ELSE 'symbol:' || r.symbol_id END, '', '', 'code_refs', r.id, '', '', r.kind, '', '',
r.start_byte, r.end_byte, r.start_row, r.start_col, r.end_row, r.end_col, '', '', '[]', '[]', '[]', '', 0, 0, '', '', '[]', '[]', '[]', '{}', '', '',
r.name || ' ' || r.kind || ' ' || f.path
FROM code_refs r JOIN code_files f ON f.id = r.file_id`,

		`INSERT INTO gj_code (` + gjCodeColumns + `)
SELECT 'import:' || i.id, 'import', i.path, i.path, i.alias, f.path, f.abs_path, i.language, f.hash, '', '', '', '',
'', '', '', '', '', '', '', '',
'file:' || i.file_id, '', '', '', 'code_imports', i.id, '', '', '', i.path, i.alias,
i.start_byte, i.end_byte, i.start_row, i.start_col, i.end_row, i.end_col, '', '', '[]', '[]', '[]', '', 0, 0, '', '', '[]', '[]', '[]', '{}', '', '',
i.path || ' ' || i.alias || ' ' || f.path
FROM code_imports i JOIN code_files f ON f.id = i.file_id`,

		`INSERT INTO gj_code (` + gjCodeColumns + `)
SELECT 'edge:' || e.id, 'edge', e.kind, e.kind, e.confidence, f.path, f.abs_path, f.language, f.hash, '', '', '', '',
'', '', '', '', '', '', '', '',
'file:' || e.file_id, CASE WHEN e.source_symbol_id IS NULL THEN '' ELSE 'symbol:' || e.source_symbol_id END, '', CASE WHEN e.target_symbol_id IS NULL THEN '' ELSE 'symbol:' || e.target_symbol_id END, 'code_edges', e.id, '', e.kind, '', '', '',
0, 0, 0, 0, 0, 0, '', '', '[]', '[]', '[]', '', 0, 0, '', '', '[]', '[]', '[]', '{}', '', '',
e.kind || ' ' || e.confidence || ' ' || f.path
FROM code_edges e JOIN code_files f ON f.id = e.file_id`,

		`INSERT INTO gj_code (` + gjCodeColumns + `)
SELECT 'db_reference:' || d.id, 'db_reference', COALESCE(NULLIF(d.column_key, ''), d.table_key), COALESCE(NULLIF(d.column_key, ''), d.table_key), d.evidence, f.path, f.abs_path, f.language, f.hash, '', '', '', '',
COALESCE(NULLIF(d.column_key, ''), d.table_key), d.database_name, d.schema_name, d.table_name, d.column_name,
CASE WHEN d.table_key = '' THEN '' ELSE 'table:' || d.table_key END,
CASE WHEN d.table_key = '' THEN '' ELSE 'table:' || d.table_key END,
CASE WHEN d.column_key = '' THEN '' ELSE 'column:' || d.column_key END,
'file:' || d.file_id, CASE WHEN d.symbol_id IS NULL THEN '' ELSE 'symbol:' || d.symbol_id END, '', '', 'code_db_refs', d.id, '', '', d.ref_kind, '', '',
d.start_byte, d.end_byte, d.start_row, d.start_col, d.end_row, d.end_col, '', '', '[]', '[]', '[]', '', 0, 0, '', '', '[]', '[]', '[]',
json_object('confidence', d.confidence, 'evidence', d.evidence, 'source_text', d.source_text, 'resolved', d.resolved, 'ambiguous', d.ambiguous), '', '',
d.database_name || ' ' || d.schema_name || ' ' || d.table_name || ' ' || d.column_name || ' ' || d.source_text || ' ' || f.path
FROM code_db_refs d JOIN code_files f ON f.id = d.file_id`,

		`INSERT INTO gj_code (` + gjCodeColumns + `)
SELECT 'injection:' || j.id, 'injection', j.language, j.language, '', f.path, f.abs_path, j.language, f.hash, '', '', '', '',
'', '', '', '', '', '', '', '',
'file:' || j.file_id, '', '', CASE WHEN j.virtual_file_id IS NULL THEN '' ELSE 'file:' || j.virtual_file_id END, 'code_injections', j.id, '', '', '', '', '',
j.start_byte, j.end_byte, j.start_row, j.start_col, j.end_row, j.end_col, '', '', '[]', '[]', '[]', '', 0, 0, '', '', '[]', '[]', '[]', '{}', '', '',
j.language || ' ' || j.content || ' ' || f.path
FROM code_injections j JOIN code_files f ON f.id = j.file_id`,

		`INSERT INTO gj_code (` + gjCodeColumns + `)
SELECT 'doc:' || d.id, 'doc', substr(d.text, 1, 80), substr(d.text, 1, 80), d.text, f.path, f.abs_path, f.language, f.hash, '', '', '', d.text,
'', '', '', '', '', '', '', '',
'file:' || d.file_id, CASE WHEN d.symbol_id IS NULL THEN '' ELSE 'symbol:' || d.symbol_id END, '', '', 'code_docs', d.id, '', '', '', '', '',
0, 0, d.start_row, d.start_col, 0, 0, '', '', '[]', '[]', '[]', '', 0, 0, '', '', '[]', '[]', '[]', '{}', '', '',
d.text || ' ' || f.path
FROM code_docs d JOIN code_files f ON f.id = d.file_id`,

		`INSERT INTO gj_code (` + gjCodeColumns + `)
SELECT 'text_chunk:' || c.id, 'text_chunk', f.path || ':' || c.chunk_index, f.path || ':' || c.chunk_index, c.text, f.path, f.abs_path, f.language, f.hash, '', '', '', '',
'', '', '', '', '', '', '', '',
'file:' || c.file_id, '', '', '', 'code_file_text_chunks', c.id, '', '', '', '', '',
c.start_byte, c.end_byte, 0, 0, 0, 0, '', '', '[]', '[]', '[]', '', 0, 0, '', '', '[]', '[]', '[]', '{}', '', '',
c.text || ' ' || f.path
FROM code_file_text_chunks c JOIN code_files f ON f.id = c.file_id`,

		`INSERT INTO gj_code (` + gjCodeColumns + `)
SELECT 'parse_error:' || p.id, 'parse_error', p.path, p.path, p.message, p.path, '', p.language, '', '', '', '', '',
'', '', '', '', '', '', '', '',
CASE WHEN p.file_id IS NULL THEN '' ELSE 'file:' || p.file_id END, '', '', '', 'code_parse_errors', p.id, '', '', '', '', '',
0, 0, p.start_row, p.start_col, 0, 0, '', '', '[]', '[]', '[]', '', 0, 0, 'error', '', '[]', '[]', '[]', '{}', p.created_at, p.created_at,
p.path || ' ' || p.message || ' ' || p.language
FROM code_parse_errors p`,

		`INSERT INTO gj_code (` + gjCodeColumns + `)
SELECT 'ast_node:' || n.id, 'ast_node', n.node_type, n.node_type, n.field_name, f.path, f.abs_path, f.language, f.hash, '', '', '', '',
'', '', '', '', '', '', '', '',
'file:' || n.file_id, '', CASE WHEN n.parent_node_id IS NULL THEN '' ELSE 'ast_node:' || n.parent_node_id END, '', 'code_nodes', n.id, n.node_type, '', '', '', '',
n.start_byte, n.end_byte, n.start_row, n.start_col, n.end_row, n.end_col, '', '', '[]', '[]', '[]', '', 0, 0, '', '', '[]', '[]', '[]', '{}', '', '',
n.node_type || ' ' || n.field_name || ' ' || f.path
FROM code_nodes n JOIN code_files f ON f.id = n.file_id`,

		`INSERT INTO gj_code (` + gjCodeColumns + `)
SELECT 'capture:' || c.id, 'capture', c.capture_name, c.capture_name, c.text, f.path, f.abs_path, f.language, f.hash, '', '', '', '',
'', '', '', '', '', '', '', '',
'file:' || c.file_id, '', CASE WHEN c.node_id IS NULL THEN '' ELSE 'ast_node:' || c.node_id END, '', 'code_captures', c.id, '', '', c.query_kind, '', '',
c.start_byte, c.end_byte, c.start_row, c.start_col, c.end_row, c.end_col, '', '', '[]', '[]', '[]', '', 0, 0, '', '', '[]', '[]', '[]', '{}', '', '',
c.capture_name || ' ' || c.text || ' ' || f.path
FROM code_captures c JOIN code_files f ON f.id = c.file_id`,

		`INSERT INTO gj_code (` + gjCodeColumns + `)
SELECT 'index_status:' || s.id, 'index_status', s.root, s.root, s.error, s.root, s.cache_path, '', '', '', '', '', '',
'', '', '', '', '', '', '', '',
'', '', '', '', 'code_index_status', s.id, '', '', '', '', '',
0, 0, 0, 0, 0, 0, '', '', '[]', '[]', '[]', '', 0, 0, CASE WHEN s.finished_at IS NULL THEN 'running' ELSE 'finished' END, '', '[]', '[]', '[]',
json_object('files_seen', s.files_seen, 'files_added', s.files_added, 'files_changed', s.files_changed, 'files_deleted', s.files_deleted, 'files_skipped', s.files_skipped, 'parse_errors', s.parse_errors),
s.started_at, COALESCE(s.finished_at, s.started_at),
s.root || ' ' || s.cache_path || ' ' || s.error
FROM code_index_status s`,

		`INSERT INTO gj_code (` + gjCodeColumns + `)
SELECT 'change_set:' || c.id, 'change_set', c.title, c.title, c.status, '', '', '', '', '', '', '', '',
'', '', '', '', '', '', '', '',
'', '', '', '', 'code_change_sets', c.id, '', '', '', '', '',
0, 0, 0, 0, 0, 0, c.action, c.owner, c.edits, c.lock_tokens, '[]', '', 0, 0, c.status, c.diff, c.errors, c.files_changed, c.files_reindexed, '{}', c.created_at, c.updated_at,
c.title || ' ' || c.status || ' ' || c.diff
FROM code_change_sets c`,

		`INSERT INTO gj_code (` + gjCodeColumns + `)
SELECT 'lock:' || l.id, 'lock', l.path, l.path, l.status, l.path, '', '', '', '', '', '', '',
'', '', '', '', '', '', '', '',
'', '', '', '', 'code_locks', l.id, '', '', '', '', '',
0, 0, 0, 0, 0, 0, l.action, l.owner, '[]', '[]', l.ranges, l.lease_token, l.ttl_seconds, l.whole_file, l.status, '', '[]', '[]', '[]', '{}', l.created_at, l.updated_at,
l.path || ' ' || l.status || ' ' || l.owner
FROM code_locks l`,
	}
}
