package bigquerydriver

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	bq "google.golang.org/api/bigquery/v2"
)

// rows walks a finished query's result, fetching the next page only when the
// current one is used up.
type rows struct {
	ctx    context.Context
	st     *state
	job    *bq.JobReference
	fields []*bq.TableFieldSchema
	cols   []string
	page   []*bq.TableRow
	pos    int
	token  string
}

func newRows(ctx context.Context, st *state, res *queryResult) *rows {
	r := &rows{ctx: ctx, st: st, job: res.job, page: res.rows, token: res.token}
	if res.schema != nil {
		r.fields = res.schema.Fields
		r.cols = make([]string, len(r.fields))
		for i, f := range r.fields {
			r.cols[i] = f.Name
		}
	}
	return r
}

func (r *rows) Columns() []string { return r.cols }

func (r *rows) Close() error {
	r.page, r.token = nil, ""
	return nil
}

func (r *rows) Next(dest []driver.Value) error {
	for r.pos >= len(r.page) {
		if r.token == "" {
			return io.EOF
		}
		if r.job == nil {
			return fmt.Errorf("bigquery results: more rows remain, but BigQuery returned no job to read them from")
		}
		next, err := r.st.page(r.ctx, r.job, r.token)
		if err != nil {
			return apiError("results", err)
		}
		r.page, r.pos, r.token = next.Rows, 0, next.PageToken
	}
	row := r.page[r.pos]
	r.pos++
	for i := range dest {
		if i >= len(row.F) || i >= len(r.fields) || row.F[i] == nil {
			dest[i] = nil
			continue
		}
		v, err := columnValue(r.fields[i], row.F[i].V)
		if err != nil {
			return fmt.Errorf("bigquery results: column %q: %w", r.fields[i].Name, err)
		}
		dest[i] = v
	}
	return nil
}

// ColumnTypeDatabaseTypeName reports the BigQuery type of a column.
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	if i < 0 || i >= len(r.fields) {
		return ""
	}
	f := r.fields[i]
	if f.Mode == "REPEATED" {
		return "ARRAY<" + strings.ToUpper(f.Type) + ">"
	}
	return strings.ToUpper(f.Type)
}

// columnValue converts one cell by the type in the result's schema. The REST
// API sends every scalar as text, so guessing from the text would turn a
// STRING "007" into the number 7.
//
// Arrays and structs become JSON text. NUMERIC, DATE, DATETIME, TIME, JSON,
// GEOGRAPHY and BYTES (base64) stay as the text BigQuery sent.
func columnValue(f *bq.TableFieldSchema, v any) (driver.Value, error) {
	if v == nil {
		return nil, nil
	}
	if f.Mode == "REPEATED" || isRecord(f.Type) {
		nested, err := nestedValue(f, v, false)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(nested)
		if err != nil {
			return nil, err
		}
		return string(data), nil
	}
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected %T for %s", v, f.Type)
	}
	switch strings.ToUpper(f.Type) {
	case "INTEGER", "INT64":
		return strconv.ParseInt(s, 10, 64)
	case "FLOAT", "FLOAT64":
		return strconv.ParseFloat(s, 64)
	case "BOOLEAN", "BOOL":
		return strconv.ParseBool(s)
	case "TIMESTAMP":
		return parseTimestamp(s)
	default:
		return s, nil
	}
}

// nestedValue decodes an array or struct cell into plain Go values. The wire
// form wraps every element as {"v": ...} and every struct as {"f": [...]}.
func nestedValue(f *bq.TableFieldSchema, v any, element bool) (any, error) {
	if v == nil {
		return nil, nil
	}
	if f.Mode == "REPEATED" && !element {
		items, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("unexpected %T for an array", v)
		}
		out := make([]any, len(items))
		for i, item := range items {
			val, err := nestedValue(f, cellValue(item), true)
			if err != nil {
				return nil, err
			}
			out[i] = val
		}
		return out, nil
	}
	if isRecord(f.Type) {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("unexpected %T for a struct", v)
		}
		cells, _ := m["f"].([]any)
		out := make(map[string]any, len(f.Fields))
		for i, sub := range f.Fields {
			if i >= len(cells) {
				break
			}
			val, err := nestedValue(sub, cellValue(cells[i]), false)
			if err != nil {
				return nil, err
			}
			out[sub.Name] = val
		}
		return out, nil
	}

	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected %T for %s", v, f.Type)
	}
	switch strings.ToUpper(f.Type) {
	case "INTEGER", "INT64":
		return strconv.ParseInt(s, 10, 64)
	case "FLOAT", "FLOAT64":
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, err
		}
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return s, nil // JSON has no NaN or Infinity
		}
		return n, nil
	case "BOOLEAN", "BOOL":
		return strconv.ParseBool(s)
	case "TIMESTAMP":
		t, err := parseTimestamp(s)
		if err != nil {
			return nil, err
		}
		return t.Format(time.RFC3339Nano), nil
	case "JSON":
		if json.Valid([]byte(s)) {
			return json.RawMessage(s), nil
		}
		return s, nil
	default:
		return s, nil
	}
}

func cellValue(cell any) any {
	if m, ok := cell.(map[string]any); ok {
		return m["v"]
	}
	return nil
}

func isRecord(typ string) bool {
	switch strings.ToUpper(typ) {
	case "RECORD", "STRUCT":
		return true
	}
	return false
}

// parseTimestamp reads a TIMESTAMP cell: microseconds since the epoch, which
// is what the driver asks for, or seconds as a float, the API's older default.
func parseTimestamp(s string) (time.Time, error) {
	if micros, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.UnixMicro(micros).UTC(), nil
	}
	secs, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid TIMESTAMP %q", s)
	}
	whole, frac := math.Modf(secs)
	return time.Unix(int64(whole), int64(math.Round(frac*1e6))*1000).UTC(), nil
}

var (
	_ driver.Rows                           = (*rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*rows)(nil)
)
