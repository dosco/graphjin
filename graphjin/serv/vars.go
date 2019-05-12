package serv

import (
	"io"
	"strconv"

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
