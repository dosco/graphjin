package serv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/dosco/super-graph/jsn"
)

func argMap(ctx context.Context, vars []byte) func(w io.Writer, tag string) (int, error) {
	return func(w io.Writer, tag string) (int, error) {
		switch tag {
		case "user_id_provider":
			if v := ctx.Value(userIDProviderKey); v != nil {
				return io.WriteString(w, v.(string))
			}
			return 0, errors.New("query requires variable $user_id_provider")

		case "user_id":
			if v := ctx.Value(userIDKey); v != nil {
				return io.WriteString(w, v.(string))
			}
			return 0, errors.New("query requires variable $user_id")

		case "user_role":
			if v := ctx.Value(userRoleKey); v != nil {
				return io.WriteString(w, v.(string))
			}
			return 0, errors.New("query requires variable $user_role")
		}

		fields := jsn.Get(vars, [][]byte{[]byte(tag)})
		if len(fields) == 0 {
			return 0, nil
		}
		v := fields[0].Value
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			fields[0].Value = v[1 : len(v)-1]
		}

		return w.Write(escQuote(fields[0].Value))
	}
}

func argList(ctx *coreContext, args [][]byte) ([]interface{}, error) {
	vars := make([]interface{}, len(args))

	var fields map[string]json.RawMessage
	var err error

	if len(ctx.req.Vars) != 0 {
		fields, _, err = jsn.Tree(ctx.req.Vars)

		if err != nil {
			return nil, err
		}
	}

	for i := range args {
		av := args[i]

		switch {
		case bytes.Equal(av, []byte("user_id")):
			if v := ctx.Value(userIDKey); v != nil {
				vars[i] = v.(string)
			} else {
				return nil, errors.New("query requires variable $user_id")
			}

		case bytes.Equal(av, []byte("user_id_provider")):
			if v := ctx.Value(userIDProviderKey); v != nil {
				vars[i] = v.(string)
			} else {
				return nil, errors.New("query requires variable $user_id_provider")
			}

		case bytes.Equal(av, []byte("user_role")):
			if v := ctx.Value(userRoleKey); v != nil {
				vars[i] = v.(string)
			} else {
				return nil, errors.New("query requires variable $user_role")
			}

		default:
			if v, ok := fields[string(av)]; ok {
				switch v[0] {
				case '[', '{':
					vars[i] = escQuote(v)
				default:
					var val interface{}
					if err := json.Unmarshal(v, &val); err != nil {
						return nil, err
					}
					vars[i] = val
				}

			} else {
				return nil, fmt.Errorf("query requires variable $%s", string(av))
			}
		}
	}

	return vars, nil
}

func escQuote(b []byte) []byte {
	f := false
	for i := range b {
		if b[i] == '\'' {
			f = true
			break
		}
	}
	if !f {
		return b
	}

	buf := &bytes.Buffer{}
	s := 0
	for i := range b {
		if b[i] == '\'' {
			buf.Write(b[s:i])
			buf.WriteString(`''`)
			s = i + 1
		}
	}
	l := len(b)
	if s < (l - 1) {
		buf.Write(b[s:l])
	}
	return buf.Bytes()
}
