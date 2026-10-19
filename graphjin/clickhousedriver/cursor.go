package clickhousedriver

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// normalizeCursor strips the security/dialect prefix, leaving selID:v0:v1:...
func normalizeCursor(prefix, cursor string) string {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return ""
	}
	if prefix != "" && strings.HasPrefix(cursor, prefix) {
		return strings.TrimPrefix(cursor, prefix)
	}
	if strings.HasPrefix(cursor, "gj-") {
		if i := strings.Index(cursor, ":"); i != -1 && i+1 < len(cursor) {
			return cursor[i+1:]
		}
	}
	return cursor
}

// encodeCursor builds the outbound cursor token from the last row's order-by
// values: <prefix><selID-hex>:<v0>:<v1>:...
func encodeCursor(ks *Keyset, last map[string]any) string {
	parts := make([]string, 0, len(ks.Columns)+1)
	parts = append(parts, fmt.Sprintf("%s%x", ks.Prefix, ks.SelID))
	for _, c := range ks.Columns {
		parts = append(parts, formatCursorValue(last[c.Col]))
	}
	return strings.Join(parts, ":")
}

func formatCursorValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(rv.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		if f == float64(int64(f)) {
			return strconv.FormatInt(int64(f), 10)
		}
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

func parseCursorValue(s string) any {
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

// buildSeekClause renders the keyset seek WHERE ladder from the inbound cursor:
//
//	(c0 OP v0) OR (c0 = v0 AND c1 OP v1) OR ...
//
// OP is > for asc / < for desc (flipped when Backward). Returns ("", false) when
// there's no cursor (first page).
func buildSeekClause(ks *Keyset, args *[]any) (string, bool) {
	norm := normalizeCursor(ks.Prefix, ks.resolvedCursor)
	if norm == "" {
		return "", false
	}
	parts := strings.Split(norm, ":")
	if len(parts) < 2 {
		return "", false
	}
	vals := parts[1:] // skip selID
	if len(vals) < len(ks.Columns) {
		return "", false
	}

	ors := make([]string, 0, len(ks.Columns))
	for i, c := range ks.Columns {
		desc := strings.HasPrefix(strings.ToLower(c.Order), "desc")
		if ks.Backward {
			desc = !desc
		}
		ands := make([]string, 0, i+1)
		for j := 0; j < i; j++ {
			ands = append(ands, eqTerm(ks.Columns[j], vals[j], args))
		}
		strict := strictTerm(c, vals[i], desc, args)
		if strict == "" {
			continue // null cursor value at an ASC level — nothing sorts strictly after here
		}
		ands = append(ands, strict)
		if len(ands) == 1 {
			ors = append(ors, ands[0])
		} else {
			ors = append(ors, "("+strings.Join(ands, " AND ")+")")
		}
	}
	if len(ors) == 0 {
		return "", false
	}
	return "(" + strings.Join(ors, " OR ") + ")", true
}

// eqTerm is the "this column equals the cursor value" predicate for a deeper
// tie-break level (NULL-aware).
func eqTerm(c OrderBy, v string, args *[]any) string {
	col := quoteIdent(c.Col)
	if v == "" && c.Nullable {
		return col + " IS NULL"
	}
	*args = append(*args, parseCursorValue(v))
	return col + " = ?"
}

// strictTerm is the "this column sorts strictly after the cursor value" predicate.
// ClickHouse orders NULLs last in both directions, so a NULL cursor value has
// nothing after it, and a non-NULL value is followed by the next non-NULLs and
// then the NULLs.
func strictTerm(c OrderBy, v string, desc bool, args *[]any) string {
	col := quoteIdent(c.Col)
	if v == "" && c.Nullable {
		return ""
	}
	op := ">"
	if desc {
		op = "<"
	}
	*args = append(*args, parseCursorValue(v))
	if c.Nullable {
		return "(" + col + " " + op + " ? OR " + col + " IS NULL)"
	}
	return col + " " + op + " ?"
}
