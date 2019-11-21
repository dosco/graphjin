package serv

import (
	"bytes"
	"fmt"
	"io"

	"github.com/dosco/super-graph/jsn"
)

func argMap(ctx *coreContext) func(w io.Writer, tag string) (int, error) {
	return func(w io.Writer, tag string) (int, error) {
		switch tag {
		case "user_id_provider":
			if v := ctx.Value(userIDProviderKey); v != nil {
				return io.WriteString(w, v.(string))
			}
			return io.WriteString(w, "null")

		case "user_id":
			if v := ctx.Value(userIDKey); v != nil {
				return io.WriteString(w, v.(string))
			}
			return io.WriteString(w, "null")

		case "user_role":
			if v := ctx.Value(userRoleKey); v != nil {
				return io.WriteString(w, v.(string))
			}
			return io.WriteString(w, "null")
		}

		fields := jsn.Get(ctx.req.Vars, [][]byte{[]byte(tag)})
		if len(fields) == 0 {
			return 0, fmt.Errorf("variable '%s' not found", tag)
		}

		return w.Write(fields[0].Value)
	}
}

func argList(ctx *coreContext, args [][]byte) []interface{} {
	vars := make([]interface{}, len(args))

	var fields map[string]interface{}
	var err error

	if len(ctx.req.Vars) != 0 {
		fields, _, err = jsn.Tree(ctx.req.Vars)

		if err != nil {
			logger.Warn().Err(err).Msg("Failed to parse variables")
		}
	}

	for i := range args {
		av := args[i]

		switch {
		case bytes.Equal(av, []byte("user_id")):
			if v := ctx.Value(userIDKey); v != nil {
				vars[i] = v.(string)
			}

		case bytes.Equal(av, []byte("user_id_provider")):
			if v := ctx.Value(userIDProviderKey); v != nil {
				vars[i] = v.(string)
			}

		case bytes.Equal(av, []byte("user_role")):
			if v := ctx.Value(userRoleKey); v != nil {
				vars[i] = v.(string)
			}

		default:
			if v, ok := fields[string(av)]; ok {
				vars[i] = v
			}
		}
	}

	return vars
}
