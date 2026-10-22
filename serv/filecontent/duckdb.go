package filecontent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SQLOptions bounds a query over file tables.
type SQLOptions struct {
	DuckDB      string        // DuckDB CLI path; "duckdb" when empty
	MaxRows     int           // rows returned; 200 when 0
	Timeout     time.Duration // 20s when 0
	MemoryLimit string        // DuckDB memory_limit; "512MB" when empty
}

// SQLResult is a query's rows, in column order.
type SQLResult struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	Truncated bool     `json:"truncated,omitempty"`
}

// ErrNoDuckDB means the DuckDB CLI is not installed where configured.
var ErrNoDuckDB = errors.New("filecontent: the DuckDB CLI is not installed")

// RunSQL runs one read-only SQL query over the given tables with the DuckDB
// CLI. Each table is a view over its Parquet file. DuckDB may read only the
// directories of those files, with external access off and its
// configuration locked before the query runs, and the query is a single
// statement wrapped as a subquery, so it can only read.
func RunSQL(ctx context.Context, tables []TableFile, query string, opts SQLOptions) (*SQLResult, error) {
	query = strings.TrimSpace(query)
	query = strings.TrimSpace(strings.TrimRight(query, ";"))
	if query == "" {
		return nil, errors.New("sql is empty")
	}
	if strings.Contains(query, ";") {
		return nil, errors.New("sql must be a single statement")
	}
	if opts.DuckDB == "" {
		opts.DuckDB = "duckdb"
	}
	bin, err := exec.LookPath(opts.DuckDB)
	if err != nil {
		return nil, ErrNoDuckDB
	}
	if opts.MaxRows <= 0 {
		opts.MaxRows = 200
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 20 * time.Second
	}
	if opts.MemoryLimit == "" {
		opts.MemoryLimit = "512MB"
	}

	dirs := map[string]bool{}
	var allowed, views []string
	for _, t := range tables {
		dir := filepath.Dir(t.Path) + string(filepath.Separator)
		if !dirs[dir] {
			dirs[dir] = true
			allowed = append(allowed, quoteString(dir))
		}
		views = append(views, fmt.Sprintf("CREATE VIEW %s AS SELECT * FROM read_parquet(%s);", quoteIdent(t.Name), quoteString(t.Path)))
	}
	var script strings.Builder
	fmt.Fprintf(&script, "SET memory_limit=%s; SET threads=2; ", quoteString(opts.MemoryLimit))
	fmt.Fprintf(&script, "SET allowed_directories=[%s]; ", strings.Join(allowed, ", "))
	script.WriteString("SET enable_external_access=false; SET autoinstall_known_extensions=false; SET autoload_known_extensions=false; SET lock_configuration=true; ")
	script.WriteString(strings.Join(views, " "))
	// The newline ends a trailing -- comment in the query.
	fmt.Fprintf(&script, " SELECT * FROM (\n%s\n) LIMIT %d;", query, opts.MaxRows+1)

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-bail", "-no-init", "-json", "-c", script.String(), ":memory:")
	cmd.Env = []string{"HOME=" + os.TempDir()}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("sql timed out after %s", opts.Timeout)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	res, err := parseJSONRows(stdout.Bytes())
	if err != nil {
		return nil, err
	}
	if len(res.Rows) > opts.MaxRows {
		res.Rows = res.Rows[:opts.MaxRows]
		res.Truncated = true
	}
	return res, nil
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func quoteString(s string) string { return `'` + strings.ReplaceAll(s, `'`, `''`) + `'` }

// parseJSONRows reads the CLI's JSON output, an array of objects, keeping
// the column order of the first row.
func parseJSONRows(out []byte) (*SQLResult, error) {
	res := &SQLResult{Columns: []string{}, Rows: [][]any{}}
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		return res, nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("reading DuckDB output: %w", err)
	}
	for i, obj := range raw {
		dec := json.NewDecoder(bytes.NewReader(obj))
		dec.UseNumber()
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		row := make([]any, 0, len(res.Columns))
		for dec.More() {
			tok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			var v any
			if err := dec.Decode(&v); err != nil {
				return nil, err
			}
			if i == 0 {
				res.Columns = append(res.Columns, tok.(string))
			}
			row = append(row, v)
		}
		res.Rows = append(res.Rows, row)
	}
	return res, nil
}
