package core

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dosco/super-graph/core/internal/psql"
	"github.com/dosco/super-graph/jsn"
)

// argList function is used to create a list of arguments to pass
// to a prepared statement.

func (sg *SuperGraph) argList(c context.Context, md psql.Metadata, vars []byte) (
	[]interface{}, error) {

	params := md.Params()
	vl := make([]interface{}, len(params))

	var fields map[string]json.RawMessage
	var err error

	if len(vars) != 0 {
		fields, _, err = jsn.Tree(vars)

		if err != nil {
			return nil, err
		}
	}

	for i, p := range params {
		switch p.Name {
		case "user_id":
			if v := c.Value(UserIDKey); v != nil {
				vl[i] = v.(string)
			} else {
				return nil, argErr(p)
			}

		case "user_id_provider":
			if v := c.Value(UserIDProviderKey); v != nil {
				vl[i] = v.(string)
			} else {
				return nil, argErr(p)
			}

		case "user_role":
			if v := c.Value(UserRoleKey); v != nil {
				vl[i] = v.(string)
			} else {
				return nil, argErr(p)
			}

		case "cursor":
			if v, ok := fields["cursor"]; ok && v[0] == '"' {
				v1, err := sg.decrypt(string(v[1 : len(v)-1]))
				if err != nil {
					return nil, err
				}
				vl[i] = v1
			} else {
				vl[i] = nil
			}

		default:
			if v, ok := fields[p.Name]; ok {
				switch {
				case p.IsArray && v[0] != '[':
					return nil, fmt.Errorf("variable '%s' should be an array of type '%s'", p.Name, p.Type)

				case p.Type == "json" && v[0] != '[' && v[0] != '{':
					return nil, fmt.Errorf("variable '%s' should be an array or object", p.Name)
				}

				switch v[0] {
				case '[', '{':
					vl[i] = v

				default:
					var val interface{}
					if err := json.Unmarshal(v, &val); err != nil {
						return nil, err
					}
					vl[i] = val
				}

			} else {
				return nil, argErr(p)
			}
		}
	}

	return vl, nil
}

func argErr(p psql.Param) error {
	return fmt.Errorf("required variable '%s' of type '%s' must be set", p.Name, p.Type)
}
