package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dosco/super-graph/core/internal/psql"
	"github.com/dosco/super-graph/jsn"
)

// argList function is used to create a list of arguments to pass
// to a prepared statement.

type args struct {
	values []interface{}
	cindx  int // index of cursor arg
}

func (sg *SuperGraph) argList(c context.Context, md psql.Metadata, vars []byte) (
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
				default:
					return ar, errors.New("user_id must be an integer or a string")
				}
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
				v1, err := sg.decrypt(string(v[1 : len(v)-1]))
				if err != nil {
					return ar, err
				}
				vl[i] = v1
			} else {
				vl[i] = nil
			}
			ar.cindx = i

		default:
			if v, ok := fields[p.Name]; ok {
				switch {
				case p.IsArray && v[0] != '[':
					return ar, fmt.Errorf("variable '%s' should be an array of type '%s'", p.Name, p.Type)

				case p.Type == "json" && v[0] != '[' && v[0] != '{':
					return ar, fmt.Errorf("variable '%s' should be an array or object", p.Name)
				}

				switch v[0] {
				case '[', '{':
					vl[i] = v

				default:
					if v[0] == '"' {
						vl[i] = string(v[1 : len(v)-1])
					} else {
						vl[i] = string(v)
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

func (sg *SuperGraph) roleQueryArgList(c context.Context) (args, error) {
	ar := args{cindx: -1}
	params := sg.roleStmtMD.Params()
	vl := make([]interface{}, len(params))

	for i, p := range params {
		switch p.Name {
		case "user_id":
			if v := c.Value(UserIDKey); v != nil {
				switch v1 := v.(type) {
				case string:
					vl[i] = v1
				case int:
					vl[i] = v1
				default:
					return ar, errors.New("user_id must be an integer or a string")
				}
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
		}
	}
	ar.values = vl
	return ar, nil
}

func argErr(p psql.Param) error {
	return fmt.Errorf("required variable '%s' of type '%s' must be set", p.Name, p.Type)
}
