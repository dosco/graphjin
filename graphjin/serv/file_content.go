package serv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/fstable"
	"github.com/dosco/graphjin/serv/v3/filecontent"
)

// FileContentConfig turns on reading inside the files of file sources. Each
// source gets an index of its files' text and Parquet copies of its sheets
// and CSV files, kept in Dir and refreshed as files change. File tables then
// gain a search argument and passages and tables columns, text covers PDF,
// Word, PowerPoint and Excel files, and the query_file_tables tool runs SQL
// over the sheets with the DuckDB CLI.
type FileContentConfig struct {
	// Enable file content indexing
	Enabled bool `mapstructure:"enabled" jsonschema:"title=Enabled"`

	// Directory for the derived index and tables; default .file-content
	// under the config directory. Safe to delete: it is rebuilt.
	Dir string `mapstructure:"dir" jsonschema:"title=Directory"`

	// DuckDB CLI for SQL over sheets; default duckdb on PATH
	DuckDB string `mapstructure:"duckdb" jsonschema:"title=DuckDB CLI"`

	// Largest file indexed, in MB; default 100
	MaxFileMB int `mapstructure:"max_file_mb" jsonschema:"title=Max File Size (MB)"`

	// How long an index stays fresh before a read checks the source for
	// changes, in seconds; default 10
	RefreshSeconds int `mapstructure:"refresh_seconds" jsonschema:"title=Refresh Interval (seconds)"`

	// Most rows a SQL query returns; default 200
	MaxRows int `mapstructure:"max_rows" jsonschema:"title=Max SQL Rows"`

	// SQL query timeout in seconds; default 20
	TimeoutSeconds int `mapstructure:"timeout_seconds" jsonschema:"title=SQL Timeout (seconds)"`
}

// fileContent implements core.FileContent over one index per file source.
type fileContent struct {
	conf    FileContentConfig
	dir     string
	backend func(name string) (fstable.Backend, bool)

	mu      sync.Mutex
	indexes map[string]*filecontent.Index
}

// fileContentOptions registers the content reader with the engine when
// file content is enabled and there are file sources.
func (s *graphjinService) fileContentOptions() []core.Option {
	fc := s.fileContentReader()
	if fc == nil {
		return nil
	}
	return []core.Option{core.OptionSetFileContent(fc)}
}

// fileContentReader returns the service's content reader, created once.
func (s *graphjinService) fileContentReader() *fileContent {
	if s != nil {
		// Scoped copies share the live service's reader.
		s = s.liveService()
	}
	if s == nil || s.conf == nil || !s.conf.FileContent.Enabled || len(s.conf.Core.Filesystems) == 0 {
		return nil
	}
	s.fileContentOnce.Do(func() {
		dir := s.conf.FileContent.Dir
		if dir == "" {
			dir = ".file-content"
		}
		if !filepath.IsAbs(dir) {
			base, err := s.basePath()
			if err != nil {
				s.log.Warnf("file content: %s", err)
				return
			}
			dir = filepath.Join(base, dir)
		}
		live := s
		s.fileContent = &fileContent{
			conf: s.conf.FileContent,
			dir:  dir,
			backend: func(name string) (fstable.Backend, bool) {
				if live.gj == nil {
					return nil, false
				}
				return live.gj.FilesystemSourceBackend(name)
			},
			indexes: map[string]*filecontent.Index{},
		}
		s.addCloseFn(s.fileContent.close)
	})
	return s.fileContent
}

// warmFileContent indexes every file source in the background, so the
// first search does not wait for a large source to be read.
func (s *graphjinService) warmFileContent() {
	fc := s.fileContentReader()
	if fc == nil {
		return
	}
	names := make([]string, 0, len(s.conf.Core.Filesystems))
	for _, f := range s.conf.Core.Filesystems {
		names = append(names, f.Name)
	}
	go func() {
		for _, name := range names {
			if _, err := fc.synced(context.Background(), name); err != nil {
				s.log.Warnf("file content: indexing %s: %s", name, err)
			}
		}
	}()
}

func (f *fileContent) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for name, ix := range f.indexes {
		ix.Close() //nolint:errcheck
		delete(f.indexes, name)
	}
}

var sourceDirName = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func (f *fileContent) index(source string) (*filecontent.Index, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ix, ok := f.indexes[source]; ok {
		return ix, nil
	}
	ix, err := filecontent.Open(filepath.Join(f.dir, sourceDirName.ReplaceAllString(source, "_")), int64(f.conf.MaxFileMB)<<20)
	if err != nil {
		return nil, err
	}
	f.indexes[source] = ix
	return ix, nil
}

// synced returns the source's index, first bringing it up to date when it
// is older than the refresh interval.
func (f *fileContent) synced(ctx context.Context, source string) (*filecontent.Index, error) {
	b, ok := f.backend(source)
	if !ok {
		return nil, fmt.Errorf("file source %q not found", source)
	}
	ix, err := f.index(source)
	if err != nil {
		return nil, err
	}
	refresh := time.Duration(f.conf.RefreshSeconds) * time.Second
	if refresh <= 0 {
		refresh = 10 * time.Second
	}
	if _, err := ix.Sync(ctx, b, refresh); err != nil {
		return nil, err
	}
	return ix, nil
}

func (f *fileContent) Text(ctx context.Context, source string, e fstable.Entry, data []byte) (string, bool, error) {
	if !filecontent.Supported(e.Key) {
		return "", false, nil
	}
	if ix, err := f.index(source); err == nil {
		if text, ok, err := ix.Text(ctx, e); err == nil && ok {
			return text, true, nil
		}
	}
	doc, err := filecontent.Extract(filepath.Base(e.Key), data)
	if errors.Is(err, filecontent.ErrUnsupported) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return doc.Text(), true, nil
}

func (f *fileContent) Search(ctx context.Context, source, query string, limit int) ([]core.FileMatch, error) {
	ix, err := f.synced(ctx, source)
	if err != nil {
		return nil, err
	}
	matches, err := ix.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]core.FileMatch, 0, len(matches))
	for _, m := range matches {
		fm := core.FileMatch{Key: m.Key, Score: m.Score, Passages: make([]core.FilePassage, 0, len(m.Passages))}
		for _, p := range m.Passages {
			fm.Passages = append(fm.Passages, core.FilePassage{Location: p.Location, Snippet: p.Snippet})
		}
		out = append(out, fm)
	}
	return out, nil
}

func (f *fileContent) Tables(ctx context.Context, source string, e fstable.Entry) ([]core.FileTable, error) {
	if !filecontent.Supported(e.Key) {
		return nil, nil
	}
	ix, err := f.synced(ctx, source)
	if err != nil {
		return nil, err
	}
	tables, err := ix.Tables(ctx, e.Key)
	if err != nil {
		return nil, err
	}
	out := make([]core.FileTable, 0, len(tables))
	for _, t := range tables {
		ft := core.FileTable{Name: t.Name, Sheet: t.Sheet, Raw: t.Raw, HeaderRow: t.HeaderRow, Rows: t.Rows}
		for _, c := range t.Columns {
			ft.Columns = append(ft.Columns, core.FileTableColumn{Name: c.Name, Type: c.Type})
		}
		out = append(out, ft)
	}
	return out, nil
}

var sqlWord = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// querySQL runs SQL over the file tables of a source. Tables are resolved
// from the words of the query; each one's file must be readable through
// canRead, which checks the caller's access the way a GraphQL read does.
func (f *fileContent) querySQL(ctx context.Context, source, query string, canRead func(ctx context.Context, key string) error) (*filecontent.SQLResult, error) {
	ix, err := f.synced(ctx, source)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var names []string
	for _, w := range sqlWord.FindAllString(query, -1) {
		w = strings.ToLower(w)
		if !seen[w] {
			seen[w] = true
			names = append(names, w)
		}
	}
	tables, err := ix.TableFiles(ctx, names)
	if err != nil {
		return nil, err
	}
	if len(tables) == 0 {
		return nil, fmt.Errorf("no file table of %s is named in the query; find table names with %s(search: ...) { key tables }", source, source)
	}
	checked := map[string]error{}
	for _, t := range tables {
		if _, ok := checked[t.Key]; !ok {
			checked[t.Key] = canRead(ctx, t.Key)
		}
		if err := checked[t.Key]; err != nil {
			return nil, fmt.Errorf("table %s: %w", t.Name, err)
		}
	}
	return filecontent.RunSQL(ctx, tables, query, filecontent.SQLOptions{
		DuckDB:  f.conf.DuckDB,
		MaxRows: f.conf.MaxRows,
		Timeout: time.Duration(f.conf.TimeoutSeconds) * time.Second,
	})
}

// fileContentSource returns the content reader and checks source names a
// file source.
func (s *graphjinService) fileContentSource(source string) (*fileContent, error) {
	fc := s.fileContentReader()
	if fc == nil {
		return nil, errors.New("file content is not enabled; set file_content.enabled")
	}
	for _, f := range s.conf.Core.Filesystems {
		if f.Name == source {
			return fc, nil
		}
	}
	return nil, fmt.Errorf("%q is not a file source", source)
}

// queryFileTables runs read-only SQL over the sheet and CSV tables of a file
// source as the caller. A table is usable only when the caller can read its
// file through GraphQL, under the same role, row rules and allow list.
func (s *graphjinService) queryFileTables(ctx context.Context, source, query string) (*filecontent.SQLResult, error) {
	fc, err := s.fileContentSource(source)
	if err != nil {
		return nil, err
	}
	if s.gj == nil {
		return nil, errors.New("graphjin is not initialised")
	}
	ctx = s.applyIdentityContext(ctx)
	check := fmt.Sprintf(`query FileTableAccess($key: String!) { %s(where: { key: { eq: $key } }) { key } }`, source)
	canRead := func(ctx context.Context, key string) error {
		vars, _ := json.Marshal(map[string]string{"key": key})
		res, err := s.gj.GraphQL(ctx, check, vars, nil)
		if err != nil {
			return err
		}
		var out map[string][]map[string]any
		if err := json.Unmarshal(res.Data, &out); err != nil || len(out[source]) == 0 {
			return fmt.Errorf("file %q is not readable", key)
		}
		return nil
	}
	return fc.querySQL(ctx, source, query, canRead)
}

// FileTablesEnabled reports whether the agent gets the query_file_tables
// tool: file content is on for this service.
func (r *serviceAgentRuntime) FileTablesEnabled() bool {
	return r != nil && r.service != nil && r.service.fileContentReader() != nil
}

// QueryFileTables runs the agent's SQL over file tables as the caller.
func (r *serviceAgentRuntime) QueryFileTables(ctx context.Context, args map[string]any) (any, error) {
	source, _ := args["source"].(string)
	query, _ := args["sql"].(string)
	if source == "" || query == "" {
		return nil, errors.New("source and sql are required")
	}
	return r.service.queryFileTables(ctx, source, query)
}
