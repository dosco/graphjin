package clickhousedriver

import (
	"database/sql"
	"fmt"
	"reflect"
	"time"
)

// scanRows reads all rows into JSON-safe column->value maps. Destinations come
// from the driver's ColumnTypes().ScanType() so clickhouse-go scans into the right
// Go type (incl. pointers for Nullable columns).
func scanRows(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}

	var out []map[string]any
	for rows.Next() {
		ptrs := make([]any, len(cols))
		for i := range cols {
			ptrs[i] = reflect.New(colTypes[i].ScanType()).Interface()
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c] = normalizeValue(reflect.ValueOf(ptrs[i]).Elem().Interface())
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// normalizeValue turns a scanned Go value into a JSON-marshalable one, unwrapping
// pointers (Nullable) and rendering time/bytes/Stringer types stably.
func normalizeValue(v any) any {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		v = rv.Elem().Interface()
	}
	switch x := v.(type) {
	case nil:
		return nil
	case time.Time:
		return x.UTC().Format(tsLayout)
	case []byte:
		return string(x)
	case fmt.Stringer:
		return x.String()
	default:
		return v
	}
}
