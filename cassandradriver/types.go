package cassandradriver

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// tsLayout is RFC3339 with millisecond precision — GraphJin's JSON timestamp form.
const tsLayout = "2006-01-02T15:04:05.000Z07:00"

// normType lowercases a CQL type and strips a frozen<...> wrapper.
func normType(t string) string {
	t = strings.TrimSpace(strings.ToLower(t))
	for strings.HasPrefix(t, "frozen<") && strings.HasSuffix(t, ">") {
		t = strings.TrimSuffix(strings.TrimPrefix(t, "frozen<"), ">")
		t = strings.TrimSpace(t)
	}
	return t
}

// genericArgs returns the comma-separated args of a parametric type, splitting at
// the top level only (so nested generics survive intact).
func genericArgs(t, prefix string) (string, bool) {
	if !strings.HasPrefix(t, prefix+"<") || !strings.HasSuffix(t, ">") {
		return "", false
	}
	return t[len(prefix)+1 : len(t)-1], true
}

func splitTop(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(s[start:]))
	return out
}

// BindValue converts a JSON-decoded value into the Go value to bind for a column
// of the given CQL type, preserving fidelity (no float precision loss on
// decimal/varint, base64 → bytes for blob, RFC3339 → time for timestamp).
func BindValue(cqlType string, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	t := normType(cqlType)

	if inner, ok := genericArgs(t, "list"); ok {
		return bindSlice(inner, v)
	}
	if inner, ok := genericArgs(t, "set"); ok {
		return bindSlice(inner, v)
	}
	if inner, ok := genericArgs(t, "map"); ok {
		kv := splitTop(inner)
		if len(kv) != 2 {
			return nil, fmt.Errorf("cassandradriver: malformed map type %q", cqlType)
		}
		return bindMap(kv[0], kv[1], v)
	}

	switch t {
	case "blob":
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("cassandradriver: blob expects a base64 string, got %T", v)
		}
		return base64.StdEncoding.DecodeString(s)
	case "timestamp":
		return bindTime(v)
	case "tinyint", "smallint", "int", "bigint", "counter":
		return toInt64(v)
	case "float", "double":
		return toFloat64(v)
	case "boolean":
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("cassandradriver: boolean expects bool, got %T", v)
		}
		return b, nil
	default:
		// uuid/timeuuid/inet/decimal/varint/text/varchar/ascii/date/time → bound as
		// their string form; the gocql layer (M2) parses string → native.
		return v, nil
	}
}

func bindSlice(elem string, v any) (any, error) {
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("cassandradriver: collection expects an array, got %T", v)
	}
	out := make([]any, len(arr))
	for i, e := range arr {
		ev, err := BindValue(elem, e)
		if err != nil {
			return nil, err
		}
		out[i] = ev
	}
	return out, nil
}

func bindMap(kt, vt string, v any) (any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("cassandradriver: map expects an object, got %T", v)
	}
	out := make(map[string]any, len(m))
	for k, e := range m {
		kv, err := BindValue(kt, k)
		if err != nil {
			return nil, err
		}
		ev, err := BindValue(vt, e)
		if err != nil {
			return nil, err
		}
		ks, ok := kv.(string)
		if !ok {
			ks = fmt.Sprint(kv)
		}
		out[ks] = ev
	}
	return out, nil
}

func bindTime(v any) (time.Time, error) {
	switch x := v.(type) {
	case string:
		if t, err := time.Parse(tsLayout, x); err == nil {
			return t, nil
		}
		return time.Parse(time.RFC3339, x)
	case float64:
		return time.UnixMilli(int64(x)).UTC(), nil
	default:
		return time.Time{}, fmt.Errorf("cassandradriver: timestamp expects RFC3339 string or epoch ms, got %T", v)
	}
}

func toInt64(v any) (int64, error) {
	switch x := v.(type) {
	case float64:
		return int64(x), nil
	case int:
		return int64(x), nil
	case int64:
		return x, nil
	default:
		return 0, fmt.Errorf("cassandradriver: integer expects a number, got %T", v)
	}
}

func toFloat64(v any) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case int:
		return float64(x), nil
	case int64:
		return float64(x), nil
	default:
		return 0, fmt.Errorf("cassandradriver: float expects a number, got %T", v)
	}
}

// DecodeValue converts a Go value scanned from Cassandra into a JSON-marshalable
// value for the given CQL type.
func DecodeValue(cqlType string, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	t := normType(cqlType)

	if inner, ok := genericArgs(t, "list"); ok {
		return decodeSlice(inner, v)
	}
	if inner, ok := genericArgs(t, "set"); ok {
		return decodeSlice(inner, v)
	}
	if inner, ok := genericArgs(t, "map"); ok {
		kv := splitTop(inner)
		if len(kv) == 2 {
			return decodeMap(kv[1], v)
		}
	}

	switch t {
	case "blob":
		if b, ok := v.([]byte); ok {
			return base64.StdEncoding.EncodeToString(b), nil
		}
		return v, nil
	case "timestamp":
		if tm, ok := v.(time.Time); ok {
			return tm.UTC().Format(tsLayout), nil
		}
		return v, nil
	default:
		// uuid/inet/decimal/varint already arrive as strings from the gocql layer;
		// numbers, bools, and text pass through unchanged.
		return v, nil
	}
}

func decodeSlice(elem string, v any) (any, error) {
	arr, ok := v.([]any)
	if !ok {
		return v, nil
	}
	out := make([]any, len(arr))
	for i, e := range arr {
		ev, err := DecodeValue(elem, e)
		if err != nil {
			return nil, err
		}
		out[i] = ev
	}
	return out, nil
}

func decodeMap(vt string, v any) (any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return v, nil
	}
	out := make(map[string]any, len(m))
	for k, e := range m {
		ev, err := DecodeValue(vt, e)
		if err != nil {
			return nil, err
		}
		out[k] = ev
	}
	return out, nil
}
