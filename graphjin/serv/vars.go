package serv

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/valyala/fasttemplate"
)

func varMap(ctx context.Context, vars variables) variables {
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

	for k, v := range vars {
		if _, ok := vm[k]; ok {
			continue
		}
		switch val := v.(type) {
		case string:
			vm[k] = val
		case int:
			vm[k] = strconv.Itoa(val)
		case int64:
			vm[k] = strconv.FormatInt(val, 64)
		case float64:
			vm[k] = fmt.Sprintf("%.0f", val)
		}
	}
	return vm
}
