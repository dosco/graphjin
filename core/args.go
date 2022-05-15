package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/dosco/graphjin/core/internal/psql"
	"github.com/dosco/graphjin/internal/jsn"
)

// argList function is used to create a list of arguments to pass
// to a prepared statement.

type args struct {
	values []interface{}
	cindx  int // index of cursor arg
}

func (gj *graphjin) argList(c context.Context, md psql.Metadata, vars []byte, rc *ReqConfig) (
	args, error) {

	ar := args{cindx: -1}
	params := md.Params()
	vl := make([]interface{}, len(params))

	var fields map[string]json.RawMessage
	var err error

	if len(vars) != 0 {
		fields, _, err = jsn.Tree(vars)
		if err != nil {
			return ar, err
		}
	}

	for i, p := range params {
		switch p.Name {
		case "user_id":
			if v := c.Value(UserIDKey); v != nil {
				switch v1 := v.(type) {
				case string:
					vl[i] = v1
				case int:
					vl[i] = v1
				case float64:
					vl[i] = int(v1)
				default:
					return ar, fmt.Errorf("user_id must be an integer or a string: %T", v)
				}
			} else {
				return ar, argErr(p)
			}

		case "user_id_raw":
			if v := c.Value(UserIDRawKey); v != nil {
				vl[i] = v.(string)
			} else {
				return ar, argErr(p)
			}

		case "user_id_provider":
			if v := c.Value(UserIDProviderKey); v != nil {
				vl[i] = v.(string)
			} else {
				return ar, argErr(p)
			}

		case "user_role":
			if v := c.Value(UserRoleKey); v != nil {
				vl[i] = v.(string)
			} else {
				return ar, argErr(p)
			}

		case "cursor":
			if v, ok := fields["cursor"]; ok && v[0] == '"' {
				v1, err := gj.decrypt(string(v[1 : len(v)-1]))
				if err != nil {
					return ar, err
				}
				vl[i] = string(v1)
			} else {
				vl[i] = nil
			}
			ar.cindx = i

		default:

			if v, ok := fields[p.Name]; ok {
				varIsNull := bytes.Equal(v, []byte("null"))

				switch {
				case p.IsNotNull && varIsNull:
					return ar, fmt.Errorf("variable '%s' cannot be null", p.Name)

				case p.IsArray && v[0] != '[' && !varIsNull:
					return ar, fmt.Errorf("variable '%s' should be an array of type '%s'", p.Name, p.Type)

				case p.Type == "json" && v[0] != '[' && v[0] != '{' && !varIsNull:
					return ar, fmt.Errorf("variable '%s' should be an array or object", p.Name)
				}
				vl[i] = parseVarVal(v)

			} else if rc != nil {
				if v, ok := rc.Vars[p.Name]; ok {
					if fn, ok := v.(func() string); ok {
						vl[i] = fn()
					}
				}

			} else {
				return ar, argErr(p)
			}
		}
	}
	ar.values = vl
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
		return string(v)
	}
}

func argErr(p psql.Param) error {
	return fmt.Errorf("required variable '%s' of type '%s' must be set", p.Name, p.Type)
}
