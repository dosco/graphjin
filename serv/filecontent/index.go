package filecontent

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/dosco/graphjin/core/v3/fstable"
	_ "modernc.org/sqlite"
)

// Index keeps the derived content of one file source: a full-text index of
// every file's units (pages, slides, sheets, sections), the extracted text,
// and the Parquet tables of its sheets and CSV files. It lives in its own
// directory and is rebuilt from the source whenever files change, so it can
// be deleted at any time.
type Index struct {
	dir          string
	db           *sql.DB
	maxFileBytes int64

	mu       sync.Mutex // serialises syncs
	lastSync time.Time
}

// DefaultMaxFileBytes caps the files the index reads.
const DefaultMaxFileBytes = 100 << 20

// nameLocation marks the indexed file name among a file's passages.
const nameLocation = "file name"

// searchChunk caps an indexed passage, so a long sheet or section ranks by
// its matching part and snippets stay near the match.
const searchChunk = 8000

// Open opens or creates the index in dir.
func Open(dir string, maxFileBytes int64) (*Index, error) {
	if maxFileBytes <= 0 {
		maxFileBytes = DefaultMaxFileBytes
	}
	if err := os.MkdirAll(filepath.Join(dir, "tables"), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "index.db")+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, stmt := range []string{
		`create table if not exists files (
			key text primary key,
			id text not null,
			base text not null,
			etag text not null,
			size integer not null,
			modified text not null,
			kind text,
			error text,
			text text
		)`,
		`create table if not exists tables (
			name text primary key,
			key text not null,
			file text not null,
			info text not null
		)`,
		`create index if not exists tables_key on tables(key)`,
		`create virtual table if not exists units using fts5(key unindexed, location unindexed, body, tokenize = 'porter unicode61')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("filecontent: index schema: %w", err)
		}
	}
	return &Index{dir: dir, db: db, maxFileBytes: maxFileBytes}, nil
}

// Close closes the index.
func (ix *Index) Close() error { return ix.db.Close() }

// SyncStats reports what a sync changed.
type SyncStats struct {
	Indexed int
	Removed int
	Failed  int
}

// Sync brings the index up to date with the source: new and changed files
// are extracted and indexed, removed files dropped. A sync that ran within
// maxAge is not repeated.
func (ix *Index) Sync(ctx context.Context, b fstable.Backend, maxAge time.Duration) (SyncStats, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	var st SyncStats
	if maxAge > 0 && time.Since(ix.lastSync) < maxAge {
		return st, nil
	}
	started := time.Now()

	known := map[string]string{}
	rows, err := ix.db.QueryContext(ctx, `select key, etag || '|' || size || '|' || modified from files`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			rows.Close()
			return st, err
		}
		known[k] = v
	}
	rows.Close()

	seen := map[string]bool{}
	after := ""
	for {
		entries, next, err := b.List(ctx, fstable.ListOpts{Limit: 1000, After: after})
		if err != nil {
			return st, err
		}
		for _, e := range entries {
			if !Supported(e.Key) {
				continue
			}
			seen[e.Key] = true
			if known[e.Key] == version(e) {
				continue
			}
			if err := ix.index(ctx, b, e); err != nil {
				if ctx.Err() != nil {
					return st, ctx.Err()
				}
				st.Failed++
			} else {
				st.Indexed++
			}
		}
		if next == "" {
			break
		}
		after = next
	}
	for k := range known {
		if !seen[k] {
			if err := ix.remove(ctx, k); err != nil {
				return st, err
			}
			st.Removed++
		}
	}
	ix.lastSync = started
	return st, nil
}

func version(e fstable.Entry) string {
	return fmt.Sprintf("%s|%d|%s", e.ETag, e.Size, e.ModifiedAt.UTC().Format(time.RFC3339Nano))
}

func fileID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}

// index extracts one file and replaces what the index holds for it. A file
// that fails to extract is recorded with its error, so it is not retried
// until it changes.
func (ix *Index) index(ctx context.Context, b fstable.Backend, e fstable.Entry) error {
	var doc *Document
	var exErr error
	if e.Size > ix.maxFileBytes {
		exErr = fmt.Errorf("file is larger than %d bytes", ix.maxFileBytes)
	} else {
		body, _, err := b.Get(ctx, e.Key)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(body, ix.maxFileBytes+1))
		body.Close()
		if err != nil {
			return err
		}
		doc, exErr = Extract(path.Base(e.Key), data)
	}

	id := fileID(e.Key)
	base, err := ix.tableBase(ctx, e.Key, id)
	if err != nil {
		return err
	}
	tableDir := filepath.Join(ix.dir, "tables", id)
	if err := os.RemoveAll(tableDir); err != nil {
		return err
	}
	var tables []Table
	if exErr == nil && len(doc.Sheets) > 0 {
		if err := os.MkdirAll(tableDir, 0o755); err != nil {
			return err
		}
		if tables, err = WriteTables(doc, tableDir, base); err != nil {
			exErr = err
			tables = nil
		}
	}

	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := deleteKey(ctx, tx, e.Key); err != nil {
		return err
	}
	var kind, errText, text any
	if exErr != nil {
		errText = exErr.Error()
	} else {
		kind, text = string(doc.Kind), doc.Text()
	}
	if _, err := tx.ExecContext(ctx,
		`insert into files (key, id, base, etag, size, modified, kind, error, text) values (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Key, id, base, e.ETag, e.Size, e.ModifiedAt.UTC().Format(time.RFC3339Nano), kind, errText, text); err != nil {
		return err
	}
	// The file name is searchable too: people ask for "the Q4 price list".
	if _, err := tx.ExecContext(ctx, `insert into units (key, location, body) values (?, ?, ?)`,
		e.Key, nameLocation, nameWords(e.Key)); err != nil {
		return err
	}
	if doc != nil {
		for _, u := range doc.Units {
			for _, part := range chunks(u.Text, searchChunk) {
				if _, err := tx.ExecContext(ctx, `insert into units (key, location, body) values (?, ?, ?)`,
					e.Key, u.Label, part); err != nil {
					return err
				}
			}
		}
	}
	for _, t := range tables {
		info, _ := json.Marshal(t)
		if _, err := tx.ExecContext(ctx, `insert into tables (name, key, file, info) values (?, ?, ?, ?)`,
			t.Name, e.Key, filepath.Join("tables", id, t.File), string(info)); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return exErr
}

// tableBase names a file's tables after its file name, made unique within
// the source.
func (ix *Index) tableBase(ctx context.Context, key, id string) (string, error) {
	stem := path.Base(key)
	stem = strings.TrimSuffix(stem, path.Ext(stem))
	base := Identifier(stem)
	var n int
	if err := ix.db.QueryRowContext(ctx, `select count(*) from files where base = ? and key <> ?`, base, key).Scan(&n); err != nil {
		return "", err
	}
	if n > 0 {
		base += "_" + id[:6]
	}
	return base, nil
}

func (ix *Index) remove(ctx context.Context, key string) error {
	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := deleteKey(ctx, tx, key); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(ix.dir, "tables", fileID(key)))
}

func deleteKey(ctx context.Context, tx *sql.Tx, key string) error {
	for _, stmt := range []string{
		`delete from files where key = ?`,
		`delete from tables where key = ?`,
		`delete from units where key = ?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, key); err != nil {
			return err
		}
	}
	return nil
}

func nameWords(key string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return ' '
	}, key)
}

// chunks splits s at line ends into pieces of about n characters.
func chunks(s string, n int) []string {
	var out []string
	for len(s) > n {
		cut := strings.LastIndexByte(s[:n], '\n')
		if cut <= 0 {
			cut = n
		}
		out = append(out, s[:cut])
		s = s[cut:]
	}
	if strings.TrimSpace(s) != "" || len(out) == 0 {
		out = append(out, s)
	}
	return out
}

// Match is a file a search found, with its best passages.
type Match struct {
	Key      string
	Score    float64
	Passages []Passage
}

// Passage is a matching part of a file.
type Passage struct {
	Location string
	Snippet  string
}

// Search ranks files against a plain-language query. Any word may match; a
// file ranks by its best passage, plus a little for each further matching
// passage, scaled by the share of the query's words found anywhere in it.
// Each file returns up to three passages.
func (ix *Index) Search(ctx context.Context, query string, limit int) ([]Match, error) {
	expr := matchExpr(query)
	if expr == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := ix.db.QueryContext(ctx, `
		select key, location, snippet(units, 2, '«', '»', ' … ', 24), bm25(units)
		from units where units match ?
		order by bm25(units) limit ?`, expr, limit*20)
	if err != nil {
		return nil, fmt.Errorf("filecontent: search: %w", err)
	}
	defer rows.Close()
	var out []Match
	byKey := map[string]int{}
	names := map[string]Passage{}
	for rows.Next() {
		var key, loc, snip string
		var rank float64
		if err := rows.Scan(&key, &loc, &snip, &rank); err != nil {
			return nil, err
		}
		score := -rank // bm25 is lower for better matches
		i, ok := byKey[key]
		if !ok {
			i = len(out)
			byKey[key] = i
			out = append(out, Match{Key: key, Score: score})
		} else {
			out[i].Score += score * 0.1
		}
		p := Passage{Location: loc, Snippet: strings.TrimSpace(snip)}
		if loc == nameLocation {
			// The name counts toward rank; it shows only when nothing in the
			// file itself matched.
			names[key] = p
			continue
		}
		if len(out[i].Passages) < 3 {
			out[i].Passages = append(out[i].Passages, p)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if len(out[i].Passages) == 0 {
			out[i].Passages = []Passage{names[out[i].Key]}
		}
	}
	// A file that holds every word of the question, even spread over its
	// pages or sheet rows, beats one page that matches a few words well.
	if terms := strings.Split(expr, " OR "); len(terms) > 1 {
		covered := map[string]int{}
		for _, term := range terms {
			rows, err := ix.db.QueryContext(ctx, `select distinct key from units where units match ?`, term)
			if err != nil {
				return nil, fmt.Errorf("filecontent: search: %w", err)
			}
			for rows.Next() {
				var key string
				if err := rows.Scan(&key); err != nil {
					rows.Close()
					return nil, err
				}
				covered[key]++
			}
			rows.Close()
		}
		for i := range out {
			f := float64(covered[out[i].Key]) / float64(len(terms))
			out[i].Score *= f * f
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// matchExpr turns free text into an FTS5 query: each word quoted, joined by
// OR, so no input is a syntax error and any word can match.
func matchExpr(q string) string {
	words := strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := map[string]bool{}
	var terms []string
	for _, w := range words {
		if seen[w] || stopWords[w] {
			continue
		}
		seen[w] = true
		terms = append(terms, `"`+w+`"`)
	}
	return strings.Join(terms, " OR ")
}

var stopWords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true, "be": true, "by": true,
	"for": true, "from": true, "how": true, "in": true, "is": true, "it": true, "of": true, "on": true,
	"or": true, "our": true, "that": true, "the": true, "this": true, "to": true, "was": true,
	"we": true, "what": true, "when": true, "which": true, "with": true,
}

// Text returns a file's extracted text when the index holds the version
// given; ok is false otherwise.
func (ix *Index) Text(ctx context.Context, e fstable.Entry) (string, bool, error) {
	var text sql.NullString
	err := ix.db.QueryRowContext(ctx,
		`select text from files where key = ? and etag || '|' || size || '|' || modified = ?`, e.Key, version(e)).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return text.String, text.Valid, nil
}

// Tables lists the SQL tables derived from a file.
func (ix *Index) Tables(ctx context.Context, key string) ([]Table, error) {
	rows, err := ix.db.QueryContext(ctx, `select info from tables where key = ? order by rowid`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Table
	for rows.Next() {
		var info string
		if err := rows.Scan(&info); err != nil {
			return nil, err
		}
		var t Table
		if err := json.Unmarshal([]byte(info), &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TableFile is where a named table's Parquet file lives and which file it
// came from.
type TableFile struct {
	Name string
	Key  string
	Path string
}

// TableFiles resolves table names to their Parquet files. Unknown names are
// left out.
func (ix *Index) TableFiles(ctx context.Context, names []string) ([]TableFile, error) {
	var out []TableFile
	for _, n := range names {
		var key, file string
		err := ix.db.QueryRowContext(ctx, `select key, file from tables where name = ?`, n).Scan(&key, &file)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, TableFile{Name: n, Key: key, Path: filepath.Join(ix.dir, file)})
	}
	return out, nil
}
