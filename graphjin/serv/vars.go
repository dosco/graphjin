package serv

import (
	"io"
	"strconv"
	"strings"

	"github.com/valyala/fasttemplate"
)

func varMap(ctx *coreContext) variables {
	userIDFn := func(w io.Writer, _ string) (int, error) {
		if v := ctx.Value(userIDKey); v != nil {
			return w.Write([]byte(v.(string)))
		}
		return 0, errNoUserID
	}

	userIDProviderFn := func(w io.Writer, _ string) (int, error) {
		if v := ctx.Value(userIDProviderKey); v != nil {
			return w.Write([]byte(v.(string)))
		}
		return 0, errNoUserID
	}

	userIDTag := fasttemplate.TagFunc(userIDFn)
	userIDProviderTag := fasttemplate.TagFunc(userIDProviderFn)

	vm := variables{
		"user_id":          userIDTag,
		"user_id_provider": userIDProviderTag,
		"USER_ID":          userIDTag,
		"USER_ID_PROVIDER": userIDProviderTag,
	}

	for k, v := range ctx.req.Vars {
		var buf []byte
		k = strings.ToLower(k)

		if _, ok := vm[k]; ok {
			continue
		}

		switch val := v.(type) {
		case string:
			vm[k] = val
		case int:
			vm[k] = strconv.AppendInt(buf, int64(val), 10)
		case int64:
			vm[k] = strconv.AppendInt(buf, val, 10)
		case float64:
			vm[k] = strconv.AppendFloat(buf, val, 'f', -1, 64)
		}
	}
	return vm
}

func varList(ctx *coreContext, args []string) []interface{} {
	vars := make([]interface{}, 0, len(args))

	for k, v := range ctx.req.Vars {
		ctx.req.Vars[strings.ToLower(k)] = v
	}

	for i := range args {
		arg := strings.ToLower(args[i])

		if arg == "user_id" {
			if v := ctx.Value(userIDKey); v != nil {
				vars = append(vars, v.(string))
			}
		}

		if arg == "user_id_provider" {
			if v := ctx.Value(userIDProviderKey); v != nil {
				vars = append(vars, v.(string))
			}
		}

		if v, ok := ctx.req.Vars[arg]; ok {
			switch val := v.(type) {
			case string:
				vars = append(vars, val)
			case int:
				vars = append(vars, strconv.FormatInt(int64(val), 10))
			case int64:
				vars = append(vars, strconv.FormatInt(int64(val), 10))
			case float64:
				vars = append(vars, strconv.FormatFloat(val, 'f', -1, 64))
			}
		}
	}

	return vars
}
