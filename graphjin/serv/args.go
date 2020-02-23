package serv

import (
	"bytes"
	"context"
	"encoding/json"
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
			return 0, argErr("user_id_provider")

		case "user_id":
			if v := ctx.Value(userIDKey); v != nil {
				return io.WriteString(w, v.(string))
			}
			return 0, argErr("user_id")

		case "user_role":
			if v := ctx.Value(userRoleKey); v != nil {
				return io.WriteString(w, v.(string))
			}
			return 0, argErr("user_role")
		}

		fields := jsn.Get(vars, [][]byte{[]byte(tag)})

		if len(fields) == 0 {
			return 0, argErr(tag)

		}
		v := fields[0].Value

		// Open and close quotes
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			fields[0].Value = v[1 : len(v)-1]
		}

		if tag == "cursor" {
			if bytes.EqualFold(v, []byte("null")) {
				return io.WriteString(w, ``)
			}
			v1, err := decrypt(string(fields[0].Value))
			if err != nil {
				return 0, err
			}

			return w.Write(v1)
		}

		fmt.Println(">>>", tag, string(v))

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
				return nil, argErr("user_id")
			}

		case bytes.Equal(av, []byte("user_id_provider")):
			if v := ctx.Value(userIDProviderKey); v != nil {
				vars[i] = v.(string)
			} else {
				return nil, argErr("user_id_provider")
			}

		case bytes.Equal(av, []byte("user_role")):
			if v := ctx.Value(userRoleKey); v != nil {
				vars[i] = v.(string)
			} else {
				return nil, argErr("user_role")
			}

		case bytes.Equal(av, []byte("cursor")):
			if v, ok := fields["cursor"]; ok && v[0] == '"' {
				v1, err := decrypt(string(v[1 : len(v)-1]))
				if err != nil {
					return nil, err
				}
				vars[i] = v1
			} else {
				return nil, argErr("cursor")
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
				return nil, argErr(string(av))
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

func argErr(name string) error {
	return fmt.Errorf("query requires variable '%s' to be set", name)
}
