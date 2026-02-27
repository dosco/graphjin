package core

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

func prepareQueryArgsForDB(dbType, query string, args []interface{}) (string, []interface{}, error) {
	if dbType != "snowflake" || len(args) == 0 {
		return query, args, nil
	}

	q, err := inlinePositionalArgs(query, args)
	if err != nil {
		return "", nil, err
	}
	return q, nil, nil
}

func inlinePositionalArgs(query string, args []interface{}) (string, error) {
	var b strings.Builder
	b.Grow(len(query) + (len(args) * 8))

	inSingle := false
	inDouble := false
	argPos := 0

	for i := 0; i < len(query); i++ {
		ch := query[i]

		switch ch {
		case '\'':
			b.WriteByte(ch)
			if !inDouble {
				if inSingle && i+1 < len(query) && query[i+1] == '\'' {
					b.WriteByte(query[i+1])
					i++
					continue
				}
				inSingle = !inSingle
			}

		case '"':
			b.WriteByte(ch)
			if !inSingle {
				if inDouble && i+1 < len(query) && query[i+1] == '"' {
					b.WriteByte(query[i+1])
					i++
					continue
				}
				inDouble = !inDouble
			}

		case '?':
			if inSingle || inDouble {
				b.WriteByte(ch)
				continue
			}
			if argPos >= len(args) {
				return "", fmt.Errorf("missing argument for placeholder at index %d", argPos)
			}
			lit, err := sqlLiteral(args[argPos])
			if err != nil {
				return "", err
			}
			b.WriteString(lit)
			argPos++

		default:
			b.WriteByte(ch)
		}
	}

	if argPos != len(args) {
		return "", fmt.Errorf("unused arguments: used %d of %d", argPos, len(args))
	}

	return b.String(), nil
}

func sqlLiteral(v interface{}) (string, error) {
	if v == nil {
		return "NULL", nil
	}

	switch x := v.(type) {
	case bool:
		if x {
			return "TRUE", nil
		}
		return "FALSE", nil
	case string:
		return quoteSQLString(x), nil
	case []byte:
		return quoteSQLString(string(x)), nil
	case json.RawMessage:
		return quoteSQLString(string(x)), nil
	case time.Time:
		return quoteSQLString(x.UTC().Format(time.RFC3339Nano)), nil
	case int:
		return strconv.FormatInt(int64(x), 10), nil
	case int8:
		return strconv.FormatInt(int64(x), 10), nil
	case int16:
		return strconv.FormatInt(int64(x), 10), nil
	case int32:
		return strconv.FormatInt(int64(x), 10), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case uint:
		return strconv.FormatUint(uint64(x), 10), nil
	case uint8:
		return strconv.FormatUint(uint64(x), 10), nil
	case uint16:
		return strconv.FormatUint(uint64(x), 10), nil
	case uint32:
		return strconv.FormatUint(uint64(x), 10), nil
	case uint64:
		return strconv.FormatUint(x, 10), nil
	case float32:
		return strconv.FormatFloat(float64(x), 'g', -1, 32), nil
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64), nil
	}

	rv := reflect.ValueOf(v)
	if rv.IsValid() && rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return "NULL", nil
		}
		return sqlLiteral(rv.Elem().Interface())
	}

	if b, err := json.Marshal(v); err == nil {
		return quoteSQLString(string(b)), nil
	}

	return quoteSQLString(fmt.Sprint(v)), nil
}

func quoteSQLString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
