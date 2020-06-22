package core

import (
	"encoding/json"
	"fmt"

	"github.com/dosco/super-graph/core/internal/psql"
	"github.com/dosco/super-graph/jsn"
)

// argList function is used to create a list of arguments to pass
// to a prepared statement.

func (c *scontext) argList(md psql.Metadata) ([]interface{}, error) {
	params := md.Params()
	vars := make([]interface{}, len(params))

	var fields map[string]json.RawMessage
	var err error

	if len(c.vars) != 0 {
		fields, _, err = jsn.Tree(c.vars)

		if err != nil {
			return nil, err
		}
	}

	for i, p := range params {
		switch p.Name {
		case "user_id":
			if v := c.Value(UserIDKey); v != nil {
				vars[i] = v.(string)
			} else {
				return nil, argErr(p)
			}

		case "user_id_provider":
			if v := c.Value(UserIDProviderKey); v != nil {
				vars[i] = v.(string)
			} else {
				return nil, argErr(p)
			}

		case "user_role":
			if v := c.Value(UserRoleKey); v != nil {
				vars[i] = v.(string)
			} else {
				return nil, argErr(p)
			}

		case "cursor":
			if v, ok := fields["cursor"]; ok && v[0] == '"' {
				v1, err := c.sg.decrypt(string(v[1 : len(v)-1]))
				if err != nil {
					return nil, err
				}
				vars[i] = v1
			} else {
				vars[i] = nil
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
					vars[i] = v

				default:
					var val interface{}
					if err := json.Unmarshal(v, &val); err != nil {
						return nil, err
					}
					vars[i] = val
				}

			} else {
				return nil, argErr(p)
			}
		}
	}

	return vars, nil
}

func argErr(p psql.Param) error {
	return fmt.Errorf("required variable '%s' of type '%s' must be set", p.Name, p.Type)
}
