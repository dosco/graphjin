package clickhousedriver

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// tsLayout is RFC3339 with millisecond precision — GraphJin's JSON timestamp form.
const tsLayout = "2006-01-02T15:04:05.000Z07:00"

// genericArgs returns the inner args of a parametric ClickHouse type such as
// Nullable(T), Array(T) or Map(K,V).
func genericArgs(t, prefix string) (string, bool) {
	if strings.HasPrefix(t, prefix+"(") && strings.HasSuffix(t, ")") {
		return strings.TrimSpace(t[len(prefix)+1 : len(t)-1]), true
	}
	return "", false
}

// chTypeToSQL maps a ClickHouse type to a GraphJin SQL type, reporting whether the
// column is an array and whether it is nullable.
func chTypeToSQL(chType string) (sqlType string, isArray, isNullable bool) {
	t := strings.TrimSpace(chType)
	for {
		if inner, ok := genericArgs(t, "Nullable"); ok {
			isNullable = true
			t = inner
			continue
		}
		if inner, ok := genericArgs(t, "LowCardinality"); ok {
			t = inner
			continue
		}
		break
	}
	if inner, ok := genericArgs(t, "Array"); ok {
		st, _, n := chTypeToSQL(inner)
		return st, true, n || isNullable
	}
	if _, ok := genericArgs(t, "Map"); ok {
		return "json", false, isNullable
	}
	if _, ok := genericArgs(t, "Tuple"); ok {
		return "json", false, isNullable
	}
	if _, ok := genericArgs(t, "Nested"); ok {
		return "json", false, isNullable
	}

	base := t
	if i := strings.IndexByte(base, '('); i >= 0 { // FixedString(N), DateTime64(...), Decimal(...)
		base = base[:i]
	}
	switch base {
	case "Int8", "Int16", "Int32", "UInt8", "UInt16", "UInt32":
		return "integer", false, isNullable
	case "Int64", "Int128", "Int256", "UInt64", "UInt128", "UInt256":
		return "bigint", false, isNullable
	case "Float32":
		return "real", false, isNullable
	case "Float64":
		return "double precision", false, isNullable
	case "Decimal", "Decimal32", "Decimal64", "Decimal128", "Decimal256":
		return "numeric", false, isNullable
	case "Bool", "Boolean":
		return "boolean", false, isNullable
	case "String", "FixedString", "IPv4", "IPv6", "Enum8", "Enum16":
		return "text", false, isNullable
	case "UUID":
		return "uuid", false, isNullable
	case "Date", "Date32":
		return "date", false, isNullable
	case "DateTime", "DateTime64":
		return "timestamptz", false, isNullable
	default:
		return "json", false, isNullable
	}
}

// parseJSONValue unwraps json.RawMessage / []byte params into Go values.
func parseJSONValue(v any) any {
	switch val := v.(type) {
	case json.RawMessage:
		var parsed any
		if err := json.Unmarshal(val, &parsed); err != nil {
			return val
		}
		return parsed
	case []byte:
		var parsed any
		if err := json.Unmarshal(val, &parsed); err != nil {
			return val
		}
		return parsed
	default:
		return v
	}
}

// parseDoc coerces a raw input param into a column->value map.
func parseDoc(v any) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		return x
	case json.RawMessage:
		var d map[string]any
		if json.Unmarshal(x, &d) == nil {
			return d
		}
	case []byte:
		var d map[string]any
		if json.Unmarshal(x, &d) == nil {
			return d
		}
	case string:
		var d map[string]any
		if json.Unmarshal([]byte(x), &d) == nil {
			return d
		}
	}
	return nil
}

func asStringVal(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case []byte:
		return string(x), true
	case json.RawMessage:
		var s string
		if json.Unmarshal(x, &s) == nil {
			return s, true
		}
	}
	return "", false
}

func toInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return int(i)
		}
	case string:
		if i, err := strconv.Atoi(x); err == nil {
			return i
		}
	}
	return 0
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// coerceForCol converts a JSON-decoded value to the Go type clickhouse-go binds
// for the column's GraphJin SQL type (JSON numbers arrive as float64).
func coerceForCol(sqlType string, v any) any {
	f, ok := v.(float64)
	if !ok {
		return v
	}
	switch sqlType {
	case "integer", "bigint":
		return int64(f)
	}
	return v
}

func asBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case uint8:
		return x != 0
	case int:
		return x != 0
	case int64:
		return x != 0
	case uint64:
		return x != 0
	case float64:
		return x != 0
	}
	return false
}
