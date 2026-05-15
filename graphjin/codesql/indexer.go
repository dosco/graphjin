//go:build cgo

package codesql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	ts "github.com/tree-sitter/go-tree-sitter"
	_ "modernc.org/sqlite"
)

const maxIndexedFileSize = 2 << 20 // 2 MiB

// OpenManaged opens or creates the managed SQLite cache, reconciles it with the
// source root, and optionally starts the runtime watcher.
func OpenManaged(ctx context.Context, opts Options) (*Managed, *Stats, error) {
	if strings.TrimSpace(opts.Root) == "" {
		return nil, nil, fmt.Errorf("codesql source path is required")
	}
	if opts.Name == "" {
		opts.Name = "default"
	}
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, nil, err
	}
	if err := ensureDir(opts.CacheDir); err != nil {
		return nil, nil, err
	}
	cachePath, err := CachePath(opts.Name, root, opts.CacheDir)
	if err != nil {
		return nil, nil, err
	}
	db, err := sql.Open("sqlite", cachePath)
	if err != nil {
		return nil, nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	if err := ensureSchema(ctx, db); err != nil {
		db.Close() //nolint:errcheck
		return nil, nil, err
	}

	languages := commonLanguages()
	if len(languages) == 0 {
		db.Close() //nolint:errcheck
		return nil, nil, fmt.Errorf("codesql requires a cgo-enabled build with bundled tree-sitter grammars")
	}

	idx := &indexer{
		db:            db,
		root:          root,
		cachePath:     cachePath,
		languages:     languages,
		inferDBRefs:   opts.InferDBRefs,
		dbRefResolver: newDBRefResolver(opts.RefTargets),
	}
	stats, err := idx.Reconcile(ctx)
	if err != nil {
		db.Close() //nolint:errcheck
		return nil, nil, err
	}

	m := &Managed{DB: db, CachePath: cachePath, Root: root, refTargets: idx, reconciler: idx}
	m.notifier = idx
	if err := m.loadActiveLocks(ctx); err != nil {
		db.Close() //nolint:errcheck
		return nil, nil, err
	}
	if opts.Watch {
		wctx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel
		m.done = make(chan struct{})
		go func() {
			defer close(m.done)
			_ = idx.Watch(wctx)
		}()
	}
	return m, stats, nil
}

type indexer struct {
	mu             sync.Mutex
	hookMu         sync.RWMutex
	db             *sql.DB
	root           string
	cachePath      string
	languages      []languageSpec
	inferDBRefs    bool
	dbRefResolver  *dbRefResolver
	onSourceChange func()
}

func (idx *indexer) Reconcile(ctx context.Context) (*Stats, error) {
	idx.mu.Lock()
	var notifySourceChange bool
	defer func() {
		if notifySourceChange {
			idx.fireSourceChangeHook()
		}
	}()
	defer idx.mu.Unlock()
	start := time.Now()
	stats := &Stats{}
	statusID, err := idx.startStatus(ctx)
	if err != nil {
		return nil, err
	}
	if err := idx.upsertLanguageMetadata(ctx); err != nil {
		idx.finishStatus(ctx, statusID, stats, err)
		return nil, err
	}
	if !idx.inferDBRefs {
		if err := idx.clearDBRefs(ctx); err != nil {
			idx.finishStatus(ctx, statusID, stats, err)
			return nil, err
		}
	}

	seen := make(map[string]struct{})
	err = filepath.WalkDir(idx.root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == idx.cachePath {
			return nil
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) || strings.HasPrefix(path, filepath.Join(idx.root, "config", "codesql")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(idx.root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if isMarkdownPath(path) {
			seen[rel] = struct{}{}
			stats.FilesIndexed++
			action, err := idx.reconcileMarkdownFile(ctx, path, rel)
			if err != nil {
				stats.ParseErrors++
				return nil
			}
			switch action {
			case "added":
				stats.FilesAdded++
			case "changed":
				stats.FilesChanged++
			case "skipped":
				stats.FilesSkipped++
			}
			return nil
		}
		if language, ok := refOnlyLanguage(path); ok {
			seen[rel] = struct{}{}
			stats.FilesIndexed++
			action, err := idx.reconcileRefOnlyFile(ctx, language, path, rel)
			if err != nil {
				stats.ParseErrors++
				return nil
			}
			switch action {
			case "added":
				stats.FilesAdded++
			case "changed":
				stats.FilesChanged++
			case "skipped":
				stats.FilesSkipped++
			}
			return nil
		}
		spec, ok := languageByExt(idx.languages, path)
		if !ok {
			return nil
		}
		seen[rel] = struct{}{}
		stats.FilesIndexed++
		action, err := idx.reconcileFile(ctx, spec, path, rel)
		if err != nil {
			stats.ParseErrors++
			return nil
		}
		switch action {
		case "added":
			stats.FilesAdded++
		case "changed":
			stats.FilesChanged++
		case "skipped":
			stats.FilesSkipped++
		}
		return nil
	})
	if err != nil {
		idx.finishStatus(ctx, statusID, stats, err)
		return nil, err
	}
	deleted, err := idx.deleteMissing(ctx, seen)
	if err != nil {
		idx.finishStatus(ctx, statusID, stats, err)
		return nil, err
	}
	stats.FilesDeleted = deleted
	if idx.inferDBRefs {
		if err := idx.linkAllDBRefs(ctx); err != nil {
			idx.finishStatus(ctx, statusID, stats, err)
			return nil, err
		}
	}
	stats.Duration = time.Since(start)
	err = idx.finishStatus(ctx, statusID, stats, nil)
	if err == nil {
		err = RefreshPublicGraph(ctx, idx.db)
	}
	if err == nil && (stats.FilesAdded != 0 || stats.FilesChanged != 0 || stats.FilesDeleted != 0) {
		notifySourceChange = true
	}
	return stats, err
}

func (idx *indexer) setSourceChangeHook(fn func()) {
	idx.hookMu.Lock()
	defer idx.hookMu.Unlock()
	idx.onSourceChange = fn
}

func (idx *indexer) fireSourceChangeHook() {
	idx.hookMu.RLock()
	fn := idx.onSourceChange
	idx.hookMu.RUnlock()
	if fn != nil {
		fn()
	}
}

func isMarkdownPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
}

func refOnlyLanguage(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".sql":
		return "sql", true
	case ".graphql", ".gql":
		return "graphql", true
	case ".yaml", ".yml", ".json", ".toml":
		return "config", true
	default:
		return "", false
	}
}

func (idx *indexer) dbRefQueryHash(base string) string {
	return base + "|dbrefs:" + strconv.FormatBool(idx.inferDBRefs)
}

func (idx *indexer) clearDBRefs(ctx context.Context) error {
	_, err := idx.db.ExecContext(ctx, `DELETE FROM code_db_refs`)
	return err
}

func (idx *indexer) reconcileFile(ctx context.Context, spec languageSpec, absPath, relPath string) (string, error) {
	info, err := os.Stat(absPath)
	if err != nil {
		return "", err
	}
	if info.Size() > maxIndexedFileSize {
		return "skipped", nil
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])
	mtime := info.ModTime().UnixNano()
	qhash := idx.dbRefQueryHash(combinedQueryHash(spec))
	ghash := grammarHash(spec)

	var existingID int64
	var oldHash, oldQHash, oldGHash string
	var oldSize, oldMTime int64
	row := idx.db.QueryRowContext(ctx, `SELECT f.id, f.hash, f.size, f.mtime_unix,
	  COALESCE((SELECT query_pack_hash FROM code_file_versions WHERE file_id = f.id ORDER BY id DESC LIMIT 1), ''),
	  COALESCE((SELECT grammar_hash FROM code_file_versions WHERE file_id = f.id ORDER BY id DESC LIMIT 1), '')
	  FROM code_files f WHERE f.path = ? AND f.is_virtual = 0`, relPath)
	err = row.Scan(&existingID, &oldHash, &oldSize, &oldMTime, &oldQHash, &oldGHash)
	if err == nil && oldHash == hashStr && oldSize == info.Size() && oldMTime == mtime && oldQHash == qhash && oldGHash == ghash {
		return "skipped", nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}

	action := "changed"
	if existingID == 0 {
		action = "added"
	}
	tx, err := idx.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	if err := idx.replaceFile(ctx, tx, spec, absPath, relPath, data, hashStr, info.Size(), mtime, ghash, qhash); err != nil {
		tx.Rollback() //nolint:errcheck
		idx.recordParseError(ctx, relPath, spec.Name, err.Error())
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return action, nil
}

func (idx *indexer) reconcileMarkdownFile(ctx context.Context, absPath, relPath string) (string, error) {
	info, err := os.Stat(absPath)
	if err != nil {
		return "", err
	}
	if info.Size() > maxIndexedFileSize {
		return "skipped", nil
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])
	mtime := info.ModTime().UnixNano()
	qhash := idx.dbRefQueryHash("markdown-fences-v1")
	ghash := "markdown"

	var existingID int64
	var oldHash, oldQHash, oldGHash string
	var oldSize, oldMTime int64
	row := idx.db.QueryRowContext(ctx, `SELECT f.id, f.hash, f.size, f.mtime_unix,
	  COALESCE((SELECT query_pack_hash FROM code_file_versions WHERE file_id = f.id ORDER BY id DESC LIMIT 1), ''),
	  COALESCE((SELECT grammar_hash FROM code_file_versions WHERE file_id = f.id ORDER BY id DESC LIMIT 1), '')
	  FROM code_files f WHERE f.path = ? AND f.is_virtual = 0`, relPath)
	err = row.Scan(&existingID, &oldHash, &oldSize, &oldMTime, &oldQHash, &oldGHash)
	if err == nil && oldHash == hashStr && oldSize == info.Size() && oldMTime == mtime && oldQHash == qhash && oldGHash == ghash {
		return "skipped", nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}

	action := "changed"
	if existingID == 0 {
		action = "added"
	}
	tx, err := idx.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	if err := idx.replaceMarkdownFile(ctx, tx, absPath, relPath, data, hashStr, info.Size(), mtime, ghash, qhash); err != nil {
		tx.Rollback() //nolint:errcheck
		idx.recordParseError(ctx, relPath, "markdown", err.Error())
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return action, nil
}

func (idx *indexer) reconcileRefOnlyFile(ctx context.Context, language, absPath, relPath string) (string, error) {
	info, err := os.Stat(absPath)
	if err != nil {
		return "", err
	}
	if info.Size() > maxIndexedFileSize {
		return "skipped", nil
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])
	mtime := info.ModTime().UnixNano()
	qhash := idx.dbRefQueryHash("codesql-dbrefs-v1")
	ghash := language

	var existingID int64
	var oldHash, oldQHash, oldGHash string
	var oldSize, oldMTime int64
	row := idx.db.QueryRowContext(ctx, `SELECT f.id, f.hash, f.size, f.mtime_unix,
	  COALESCE((SELECT query_pack_hash FROM code_file_versions WHERE file_id = f.id ORDER BY id DESC LIMIT 1), ''),
	  COALESCE((SELECT grammar_hash FROM code_file_versions WHERE file_id = f.id ORDER BY id DESC LIMIT 1), '')
	  FROM code_files f WHERE f.path = ? AND f.is_virtual = 0`, relPath)
	err = row.Scan(&existingID, &oldHash, &oldSize, &oldMTime, &oldQHash, &oldGHash)
	if err == nil && oldHash == hashStr && oldSize == info.Size() && oldMTime == mtime && oldQHash == qhash && oldGHash == ghash {
		return "skipped", nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}

	action := "changed"
	if existingID == 0 {
		action = "added"
	}
	tx, err := idx.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	if err := idx.replaceRefOnlyFile(ctx, tx, language, absPath, relPath, data, hashStr, info.Size(), mtime, ghash, qhash); err != nil {
		tx.Rollback() //nolint:errcheck
		idx.recordParseError(ctx, relPath, language, err.Error())
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return action, nil
}

func (idx *indexer) replaceFile(ctx context.Context, tx *sql.Tx, spec languageSpec, absPath, relPath string, data []byte, hash string, size, mtime int64, ghash, qhash string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := deleteIndexedFile(ctx, tx, relPath); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO code_files(path, abs_path, language, hash, size, mtime_unix, indexed_at, end_byte, end_row, end_col)
	  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, relPath, absPath, spec.Name, hash, size, mtime, now, len(data), countRows(data), lastCol(data))
	if err != nil {
		return err
	}
	fileID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO code_file_versions(file_id, hash, size, mtime_unix, grammar_hash, query_pack_hash, indexed_at)
	  VALUES (?, ?, ?, ?, ?, ?, ?)`, fileID, hash, size, mtime, ghash, qhash, now); err != nil {
		return err
	}
	if err := idx.indexFile(ctx, tx, spec, fileID, data); err != nil {
		return err
	}
	idx.insertTextChunks(ctx, tx, fileID, data) // best effort
	if idx.inferDBRefs {
		if err := idx.inferDBRefsForFile(ctx, tx, fileID, spec.Name, relPath, data); err != nil {
			return err
		}
	}
	idx.insertMarkdownInjections(ctx, tx, fileID, relPath, data)
	return nil
}

func (idx *indexer) replaceMarkdownFile(ctx context.Context, tx *sql.Tx, absPath, relPath string, data []byte, hash string, size, mtime int64, ghash, qhash string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := deleteIndexedFile(ctx, tx, relPath); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO code_files(path, abs_path, language, hash, size, mtime_unix, indexed_at, end_byte, end_row, end_col)
	  VALUES (?, ?, 'markdown', ?, ?, ?, ?, ?, ?, ?)`, relPath, absPath, hash, size, mtime, now, len(data), countRows(data), lastCol(data))
	if err != nil {
		return err
	}
	fileID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO code_file_versions(file_id, hash, size, mtime_unix, grammar_hash, query_pack_hash, indexed_at)
	  VALUES (?, ?, ?, ?, ?, ?, ?)`, fileID, hash, size, mtime, ghash, qhash, now); err != nil {
		return err
	}
	idx.insertTextChunks(ctx, tx, fileID, data)
	if idx.inferDBRefs {
		if err := idx.inferDBRefsForFile(ctx, tx, fileID, "markdown", relPath, data); err != nil {
			return err
		}
	}
	idx.insertMarkdownInjections(ctx, tx, fileID, relPath, data)
	return nil
}

func (idx *indexer) replaceRefOnlyFile(ctx context.Context, tx *sql.Tx, language, absPath, relPath string, data []byte, hash string, size, mtime int64, ghash, qhash string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := deleteIndexedFile(ctx, tx, relPath); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO code_files(path, abs_path, language, hash, size, mtime_unix, indexed_at, end_byte, end_row, end_col)
	  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, relPath, absPath, language, hash, size, mtime, now, len(data), countRows(data), lastCol(data))
	if err != nil {
		return err
	}
	fileID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO code_file_versions(file_id, hash, size, mtime_unix, grammar_hash, query_pack_hash, indexed_at)
	  VALUES (?, ?, ?, ?, ?, ?, ?)`, fileID, hash, size, mtime, ghash, qhash, now); err != nil {
		return err
	}
	idx.insertTextChunks(ctx, tx, fileID, data)
	if idx.inferDBRefs {
		return idx.inferDBRefsForFile(ctx, tx, fileID, language, relPath, data)
	}
	return nil
}

type codeSQLExecer interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
}

func deleteIndexedFile(ctx context.Context, q codeSQLExecer, relPath string) error {
	rows, err := q.QueryContext(ctx, `SELECT id FROM code_files WHERE path = ? OR parent_file_id IN (SELECT id FROM code_files WHERE path = ?)`, relPath, relPath)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close() //nolint:errcheck
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		for _, table := range indexedFileChildTables {
			if _, err := q.ExecContext(ctx, `DELETE FROM `+table+` WHERE file_id = ?`, id); err != nil {
				return err
			}
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM code_injections WHERE virtual_file_id = ?`, id); err != nil {
			return err
		}
	}
	for _, id := range ids {
		if _, err := q.ExecContext(ctx, `DELETE FROM code_files WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

var indexedFileChildTables = []string{
	"code_file_versions",
	"code_parse_errors",
	"code_nodes",
	"code_captures",
	"code_symbols",
	"code_scopes",
	"code_locals",
	"code_refs",
	"code_imports",
	"code_edges",
	"code_db_refs",
	"code_injections",
	"code_docs",
	"code_file_text_chunks",
}

func (idx *indexer) indexFile(ctx context.Context, tx *sql.Tx, spec languageSpec, fileID int64, data []byte) error {
	parser := ts.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(spec.Language); err != nil {
		return err
	}
	tree := parser.Parse(data, nil)
	if tree == nil {
		return fmt.Errorf("tree-sitter returned nil tree")
	}
	defer tree.Close()
	root := tree.RootNode()

	nodeIDs := make(map[uintptr]int64)
	if _, err := insertNode(ctx, tx, fileID, nil, *root, 0, 0, "", nodeIDs); err != nil {
		return err
	}
	if root.HasError() {
		pos := root.StartPosition()
		if _, err := tx.ExecContext(ctx, `INSERT INTO code_parse_errors(file_id, path, language, message, start_row, start_col, created_at)
		  SELECT ?, path, language, ?, ?, ?, ? FROM code_files WHERE id = ?`,
			fileID, "tree contains parse errors", pos.Row, pos.Column, time.Now().UTC().Format(time.RFC3339Nano), fileID); err != nil {
			return err
		}
	}

	for _, qp := range spec.QueryPacks {
		query, qerr := ts.NewQuery(spec.Language, qp.Source)
		if qerr != nil {
			return fmt.Errorf("%s query %s: %w", spec.Name, qp.Kind, qerr)
		}
		if err := idx.runQueryPack(ctx, tx, spec, fileID, data, nodeIDs, qp.Kind, query, root); err != nil {
			query.Close()
			return err
		}
		query.Close()
	}
	return nil
}

func insertNode(ctx context.Context, tx *sql.Tx, fileID int64, parentID *int64, node ts.Node, ordinal, depth int, fieldName string, nodeIDs map[uintptr]int64) (int64, error) {
	start := node.StartPosition()
	end := node.EndPosition()
	var pid any
	if parentID != nil {
		pid = *parentID
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO code_nodes(file_id, parent_node_id, ordinal, field_name, node_type, grammar_name,
	  is_named, is_extra, has_error, is_error, is_missing, start_byte, end_byte, start_row, start_col, end_row, end_col, depth)
	  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		fileID, pid, ordinal, fieldName, node.Kind(), node.GrammarName(), node.IsNamed(), node.IsExtra(), node.HasError(), node.IsError(), node.IsMissing(),
		node.StartByte(), node.EndByte(), start.Row, start.Column, end.Row, end.Column, depth)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	nodeIDs[node.Id()] = id
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		fname := node.FieldNameForChild(uint32(i))
		if _, err := insertNode(ctx, tx, fileID, &id, *child, int(i), depth+1, fname, nodeIDs); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func (idx *indexer) runQueryPack(ctx context.Context, tx *sql.Tx, spec languageSpec, fileID int64, data []byte, nodeIDs map[uintptr]int64, kind string, query *ts.Query, root *ts.Node) error {
	cursor := ts.NewQueryCursor()
	defer cursor.Close()
	matches := cursor.Matches(query, root, data)
	captureNames := query.CaptureNames()
	for match := matches.Next(); match != nil; match = matches.Next() {
		caps := make(map[string]ts.QueryCapture)
		for _, cap := range match.Captures {
			name := captureNames[cap.Index]
			caps[name] = cap
			if err := idx.insertCapture(ctx, tx, fileID, nodeIDs, kind, name, int(match.PatternIndex), cap, data); err != nil {
				return err
			}
		}
		if kind == "codesql" {
			if err := idx.insertNormalized(ctx, tx, spec, fileID, nodeIDs, caps, data); err != nil {
				return err
			}
		}
		if kind == "locals" {
			if err := idx.insertLocals(ctx, tx, fileID, nodeIDs, caps, data); err != nil {
				return err
			}
		}
	}
	return nil
}

func (idx *indexer) insertCapture(ctx context.Context, tx *sql.Tx, fileID int64, nodeIDs map[uintptr]int64, kind, name string, pattern int, cap ts.QueryCapture, data []byte) error {
	nodeID := nullableNodeID(nodeIDs, cap.Node)
	start := cap.Node.StartPosition()
	end := cap.Node.EndPosition()
	_, err := tx.ExecContext(ctx, `INSERT INTO code_captures(file_id, node_id, query_kind, capture_name, pattern_index, text,
	  start_byte, end_byte, start_row, start_col, end_row, end_col)
	  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		fileID, nodeID, kind, name, pattern, clip(cap.Node.Utf8Text(data), 4000), cap.Node.StartByte(), cap.Node.EndByte(), start.Row, start.Column, end.Row, end.Column)
	return err
}

func (idx *indexer) insertNormalized(ctx context.Context, tx *sql.Tx, spec languageSpec, fileID int64, nodeIDs map[uintptr]int64, caps map[string]ts.QueryCapture, data []byte) error {
	if nameCap, ok := caps["symbol.name"]; ok {
		defCap := nameCap
		if c, ok := caps["symbol.def"]; ok {
			defCap = c
		}
		kind := symbolKind(caps, defCap.Node.Kind())
		doc := captureText(caps, "doc", data)
		start := defCap.Node.StartPosition()
		end := defCap.Node.EndPosition()
		_, err := tx.ExecContext(ctx, `INSERT INTO code_symbols(file_id, language, kind, name, qualified_name, signature, doc, node_id,
		  start_byte, end_byte, start_row, start_col, end_row, end_col)
		  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			fileID, spec.Name, kind, cleanName(nameCap.Node.Utf8Text(data)), "", clip(defCap.Node.Utf8Text(data), 2000), doc,
			nullableNodeID(nodeIDs, defCap.Node), defCap.Node.StartByte(), defCap.Node.EndByte(), start.Row, start.Column, end.Row, end.Column)
		if err != nil {
			return err
		}
	}
	if pathCap, ok := caps["import.path"]; ok {
		defCap := pathCap
		if c, ok := caps["import.def"]; ok {
			defCap = c
		}
		alias := captureText(caps, "import.alias", data)
		start := defCap.Node.StartPosition()
		end := defCap.Node.EndPosition()
		_, err := tx.ExecContext(ctx, `INSERT INTO code_imports(file_id, language, path, alias, node_id,
		  start_byte, end_byte, start_row, start_col, end_row, end_col)
		  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			fileID, spec.Name, cleanImportPath(pathCap.Node.Utf8Text(data)), cleanName(alias), nullableNodeID(nodeIDs, defCap.Node),
			defCap.Node.StartByte(), defCap.Node.EndByte(), start.Row, start.Column, end.Row, end.Column)
		if err != nil {
			return err
		}
	}
	if refCap, ok := caps["ref.name"]; ok {
		kind := refKind(caps)
		start := refCap.Node.StartPosition()
		end := refCap.Node.EndPosition()
		res, err := tx.ExecContext(ctx, `INSERT INTO code_refs(file_id, name, kind, node_id,
		  start_byte, end_byte, start_row, start_col, end_row, end_col)
		  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			fileID, cleanName(refCap.Node.Utf8Text(data)), kind, nullableNodeID(nodeIDs, refCap.Node),
			refCap.Node.StartByte(), refCap.Node.EndByte(), start.Row, start.Column, end.Row, end.Column)
		if err != nil {
			return err
		}
		refID, _ := res.LastInsertId()
		_, _ = tx.ExecContext(ctx, `INSERT INTO code_edges(file_id, ref_id, kind, confidence) VALUES (?, ?, ?, 'syntax')`, fileID, refID, kind)
	}
	if docCap, ok := caps["doc"]; ok {
		start := docCap.Node.StartPosition()
		_, err := tx.ExecContext(ctx, `INSERT INTO code_docs(file_id, text, node_id, start_row, start_col)
		  VALUES (?, ?, ?, ?, ?)`, fileID, clip(docCap.Node.Utf8Text(data), 4000), nullableNodeID(nodeIDs, docCap.Node), start.Row, start.Column)
		if err != nil {
			return err
		}
	}
	return nil
}

func (idx *indexer) insertLocals(ctx context.Context, tx *sql.Tx, fileID int64, nodeIDs map[uintptr]int64, caps map[string]ts.QueryCapture, data []byte) error {
	if scopeCap, ok := caps["scope"]; ok {
		start := scopeCap.Node.StartPosition()
		end := scopeCap.Node.EndPosition()
		_, err := tx.ExecContext(ctx, `INSERT INTO code_scopes(file_id, kind, node_id, start_byte, end_byte, start_row, start_col, end_row, end_col)
		  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, fileID, scopeCap.Node.Kind(), nullableNodeID(nodeIDs, scopeCap.Node),
			scopeCap.Node.StartByte(), scopeCap.Node.EndByte(), start.Row, start.Column, end.Row, end.Column)
		if err != nil {
			return err
		}
	}
	if localCap, ok := caps["local.name"]; ok {
		start := localCap.Node.StartPosition()
		end := localCap.Node.EndPosition()
		_, err := tx.ExecContext(ctx, `INSERT INTO code_locals(file_id, name, kind, node_id, start_byte, end_byte, start_row, start_col, end_row, end_col)
		  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, fileID, cleanName(localCap.Node.Utf8Text(data)), localCap.Node.Kind(), nullableNodeID(nodeIDs, localCap.Node),
			localCap.Node.StartByte(), localCap.Node.EndByte(), start.Row, start.Column, end.Row, end.Column)
		if err != nil {
			return err
		}
	}
	return nil
}

func nullableNodeID(nodeIDs map[uintptr]int64, node ts.Node) any {
	if id, ok := nodeIDs[node.Id()]; ok {
		return id
	}
	return nil
}

func symbolKind(caps map[string]ts.QueryCapture, fallback string) string {
	for name := range caps {
		if strings.HasPrefix(name, "symbol.kind.") {
			return strings.TrimPrefix(name, "symbol.kind.")
		}
	}
	return fallback
}

func refKind(caps map[string]ts.QueryCapture) string {
	for name := range caps {
		if strings.HasPrefix(name, "ref.kind.") {
			return strings.TrimPrefix(name, "ref.kind.")
		}
	}
	return "reference"
}

func captureText(caps map[string]ts.QueryCapture, name string, data []byte) string {
	if cap, ok := caps[name]; ok {
		return clip(cap.Node.Utf8Text(data), 4000)
	}
	return ""
}

func cleanName(s string) string {
	return strings.Trim(strings.TrimSpace(s), `"'`)
}

func cleanImportPath(s string) string {
	return strings.Trim(strings.TrimSpace(s), `"'`)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func countRows(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	n := 0
	for _, b := range data {
		if b == '\n' {
			n++
		}
	}
	return n
}

func lastCol(data []byte) int {
	i := len(data) - 1
	for i >= 0 && data[i] != '\n' {
		i--
	}
	return len(data) - i - 1
}

func (idx *indexer) deleteMissing(ctx context.Context, seen map[string]struct{}) (int, error) {
	rows, err := idx.db.QueryContext(ctx, `SELECT path FROM code_files WHERE is_virtual = 0`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return 0, err
		}
		if _, ok := seen[p]; !ok {
			paths = append(paths, p)
		}
	}
	for _, p := range paths {
		if err := deleteIndexedFile(ctx, idx.db, p); err != nil {
			return 0, err
		}
	}
	return len(paths), rows.Err()
}

func (idx *indexer) insertTextChunks(ctx context.Context, tx *sql.Tx, fileID int64, data []byte) {
	const chunkSize = 8192
	for start := 0; start < len(data); start += chunkSize {
		end := start + chunkSize
		if end > len(data) {
			end = len(data)
		}
		_, _ = tx.ExecContext(ctx, `INSERT INTO code_file_text_chunks(file_id, chunk_index, text, start_byte, end_byte)
		  VALUES (?, ?, ?, ?, ?)`, fileID, start/chunkSize, string(data[start:end]), start, end)
	}
}

func (idx *indexer) insertMarkdownInjections(ctx context.Context, tx *sql.Tx, fileID int64, relPath string, data []byte) {
	if !isMarkdownPath(relPath) {
		return
	}
	lines := strings.SplitAfter(string(data), "\n")
	byteOffset := 0
	inFence := false
	var lang string
	var content strings.Builder
	var startByte, startRow int
	for row, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			if !inFence {
				inFence = true
				lang = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trim, "```"), "~~~"))
				if i := strings.IndexAny(lang, " \t"); i >= 0 {
					lang = lang[:i]
				}
				startByte = byteOffset + len(line)
				startRow = row + 1
				content.Reset()
			} else {
				text := content.String()
				if lang != "" && text != "" {
					spec, supported := languageByName(idx.languages, lang)
					virtualLang := lang
					if supported {
						virtualLang = spec.Name
					}
					virtualPath := relPath + "#fence-" + strconv.Itoa(startRow) + "." + virtualLang
					textHash := sha256.Sum256([]byte(text))
					textHashStr := hex.EncodeToString(textHash[:])
					now := time.Now().UTC().Format(time.RFC3339Nano)
					res, err := tx.ExecContext(ctx, `INSERT INTO code_files(path, abs_path, language, hash, size, mtime_unix, indexed_at, is_virtual, parent_file_id, start_byte, end_byte, start_row, end_row)
					  VALUES (?, ?, ?, ?, ?, 0, ?, 1, ?, ?, ?, ?, ?)`,
						virtualPath, virtualPath, virtualLang, textHashStr, len(text), now, fileID, startByte, byteOffset, startRow, row)
					if err == nil {
						vID, _ := res.LastInsertId()
						if supported {
							_, _ = tx.ExecContext(ctx, `INSERT INTO code_file_versions(file_id, hash, size, mtime_unix, grammar_hash, query_pack_hash, indexed_at)
							  VALUES (?, ?, ?, 0, ?, ?, ?)`, vID, textHashStr, len(text), grammarHash(spec), combinedQueryHash(spec), now)
							if err := idx.indexFile(ctx, tx, spec, vID, []byte(text)); err != nil {
								_, _ = tx.ExecContext(ctx, `INSERT INTO code_parse_errors(file_id, path, language, message, start_row, start_col, created_at)
								  VALUES (?, ?, ?, ?, ?, 0, ?)`, vID, virtualPath, spec.Name, err.Error(), startRow, now)
							}
							idx.insertTextChunks(ctx, tx, vID, []byte(text))
						}
						if idx.inferDBRefs {
							_ = idx.inferDBRefsForFile(ctx, tx, vID, virtualLang, virtualPath, []byte(text))
						}
						_, _ = tx.ExecContext(ctx, `INSERT INTO code_injections(file_id, virtual_file_id, language, content, start_byte, end_byte, start_row, start_col, end_row, end_col)
						  VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, 0)`, fileID, vID, virtualLang, text, startByte, byteOffset, startRow, row)
					}
				}
				inFence = false
			}
			byteOffset += len(line)
			continue
		}
		if inFence {
			content.WriteString(line)
		}
		byteOffset += len(line)
	}
}

func (idx *indexer) recordParseError(ctx context.Context, relPath, language, message string) {
	_, _ = idx.db.ExecContext(ctx, `INSERT INTO code_parse_errors(path, language, message, created_at) VALUES (?, ?, ?, ?)`,
		relPath, language, message, time.Now().UTC().Format(time.RFC3339Nano))
}

func (idx *indexer) startStatus(ctx context.Context) (int64, error) {
	res, err := idx.db.ExecContext(ctx, `INSERT INTO code_index_status(root, cache_path, started_at) VALUES (?, ?, ?)`,
		idx.root, idx.cachePath, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (idx *indexer) finishStatus(ctx context.Context, id int64, stats *Stats, runErr error) error {
	errText := ""
	if runErr != nil {
		errText = runErr.Error()
	}
	_, err := idx.db.ExecContext(ctx, `UPDATE code_index_status
	  SET finished_at = ?, files_seen = ?, files_added = ?, files_changed = ?, files_deleted = ?, files_skipped = ?, parse_errors = ?, error = ?
	  WHERE id = ?`, time.Now().UTC().Format(time.RFC3339Nano), stats.FilesIndexed, stats.FilesAdded, stats.FilesChanged,
		stats.FilesDeleted, stats.FilesSkipped, stats.ParseErrors, errText, id)
	return err
}
