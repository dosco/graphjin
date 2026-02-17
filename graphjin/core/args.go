package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/psql"
)

// argList function is used to create a list of arguments to pass
// to a prepared statement.

type args struct {
	json   []byte
	values []interface{}
	cindxs []int // indices of cursor arg
}

func (gj *graphjinEngine) argList(c context.Context,
	md psql.Metadata,
	fields map[string]json.RawMessage,
	rc *RequestConfig,
	buildJSON bool,
	pc *psql.Compiler,
) (ar args, err error) {
	ar = args{}
	params := md.Params()
	vl := make([]interface{}, len(params))

	for i, p := range params {
		switch p.Name {
		case "user_id", "userID", "userId":
			if v := c.Value(UserIDKey); v != nil {
				switch v1 := v.(type) {
				case string:
					vl[i] = v1
				case int:
					vl[i] = v1
				case float64:
					vl[i] = int(v1)
				default:
					return ar, fmt.Errorf("%s must be an integer or a string: %T", p.Name, v)
				}
			} else {
				return ar, argErr(p)
			}

		case "user_id_raw", "userIDRaw", "userIdRaw":
			if v := c.Value(UserIDRawKey); v != nil {
				vl[i] = v.(string)
			} else {
				return ar, argErr(p)
			}

		case "user_id_provider", "userIDProvider", "userIdProvider":
			if v := c.Value(UserIDProviderKey); v != nil {
				vl[i] = v.(string)
			} else {
				return ar, argErr(p)
			}

		case "user_role", "userRole":
			if v := c.Value(UserRoleKey); v != nil {
				vl[i] = v.(string)
			} else {
				return ar, argErr(p)
			}

		case "cursor":
			if v, ok := fields["cursor"]; ok && v[0] == '"' {
				vl[i] = string(v[1 : len(v)-1])
			} else {
				vl[i] = nil
			}
			ar.cindxs = append(ar.cindxs, i)

		default:
			// Check for named cursor variables (e.g., products_cursor, users_cursor, products_cursor_1)
			if strings.Contains(p.Name, "_cursor") {
				if v, ok := fields[p.Name]; ok && len(v) > 0 && v[0] == '"' {
					vl[i] = string(v[1 : len(v)-1])
				} else {
					vl[i] = nil
				}
				ar.cindxs = append(ar.cindxs, i)
			} else if v, ok := fields[p.Name]; ok {
				varIsNull := bytes.Equal(v, []byte("null"))

				switch {
				case p.IsNotNull && varIsNull:
					return ar, fmt.Errorf("variable '%s' cannot be null", p.Name)

				case p.IsArray && v[0] != '[' && !varIsNull:
					return ar, fmt.Errorf("variable '%s' should be an array of type '%s'", p.Name, p.Type)

				case p.Type == "json" && v[0] != '[' && v[0] != '{' && !varIsNull:
					return ar, fmt.Errorf("variable '%s' should be an array or object", p.Name)
				}
				// For MySQL/MariaDB: wrap single JSON object in array for JSON_TABLE '$[*]' path
				if p.WrapInArray && v[0] == '{' {
					wrapped := make([]byte, 0, len(v)+2)
					wrapped = append(wrapped, '[')
					wrapped = append(wrapped, v...)
					wrapped = append(wrapped, ']')
					v = json.RawMessage(wrapped)
				}
				// Some databases (Oracle, MSSQL) require JSON arrays/objects to be passed as strings
				// because the drivers don't handle json.RawMessage properly
				// Also handle CLOB columns that may contain JSON data
				needsStringConversion := pc.GetDialect().RequiresJSONAsString() &&
					(p.Type == "json" || p.Type == "clob" || p.Type == "nclob") &&
					(v[0] == '[' || v[0] == '{')
				if needsStringConversion {
					vl[i] = string(v)
				} else {
					vl[i] = parseVarVal(v)
				}
				// Oracle's PL/SQL BOOLEAN can't be used in SQL WHERE clauses
				// Convert Go bool to int (1/0) before it reaches the driver
				vl[i] = convertBoolIfNeeded(pc, vl[i])

			} else if rc != nil {
				if v, ok := rc.Vars[p.Name]; ok {
					switch v1 := v.(type) {
					case (func() string):
						vl[i] = v1()
					case (func() int):
						vl[i] = v1()
					case (func() bool):
						vl[i] = convertBoolIfNeeded(pc, v1())
					default:
						vl[i] = convertBoolIfNeeded(pc, v)
					}
				}
			} else {
				return ar, argErr(p)
			}
		}
	}
	ar.values = vl

	if buildJSON && len(vl) != 0 {
		if ar.json, err = json.Marshal(vl); err != nil {
			return
		}
	}
	return ar, nil
}

func parseVarVal(v json.RawMessage) interface{} {
	switch v[0] {
	case '[', '{':
		return v

	case '"':
		return string(v[1 : len(v)-1])

	case 't', 'T':
		return true

	case 'f', 'F':
		return false

	case 'n':
		return nil

	default:
		// Try to parse as a number (for MongoDB and other document databases)
		var num json.Number
		if err := json.Unmarshal(v, &num); err == nil {
			// Try int64 first
			if i, err := num.Int64(); err == nil {
				return i
			}
			// Fall back to float64
			if f, err := num.Float64(); err == nil {
				return f
			}
		}
		return string(v)
	}
}

func argErr(p psql.Param) error {
	return fmt.Errorf("required variable '%s' of type '%s' must be set", p.Name, p.Type)
}

// convertBoolIfNeeded converts Go bool to int (1/0) for databases like Oracle
// where PL/SQL BOOLEAN cannot be used in SQL WHERE clauses
func convertBoolIfNeeded(pc *psql.Compiler, v interface{}) interface{} {
	if b, ok := v.(bool); ok && pc.GetDialect().RequiresBooleanAsInt() {
		if b {
			return 1
		}
		return 0
	}
	return v
}
