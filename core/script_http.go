package core

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/dop251/goja"
)

func (c *gcontext) httpFunc(method string, url goja.Value, args ...goja.Value) goja.Value {
	var body interface{}
	var b io.Reader
	//var headers goja.Value

	if len(args) > 0 {
		body = args[0].Export()
	}
	// if len(args) > 1 {
	// 	headers = args[1]
	// }

	u := url.Export().(string)

	if body != nil {
		switch data := body.(type) {
		case map[string]goja.Value:
		case map[string]interface{}:
		case goja.ArrayBuffer:
			b = bytes.NewBuffer(data.Bytes())
		case string:
			b = bytes.NewBufferString(data)
		case []byte:
			b = bytes.NewBuffer(data)
		default:
			panic(fmt.Errorf("http: unknown body type %T", body))
		}
	}

	req, err := http.NewRequest(method, u, b)
	if err != nil {
		panic(err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	return c.sc.vm.ToValue(string(buf))
}

func (c *gcontext) httpGetFunc(url goja.Value, args ...goja.Value) goja.Value {
	return c.httpFunc("GET", url, args...)
}

func (c *gcontext) httpPostFunc(url goja.Value, args ...goja.Value) goja.Value {
	return c.httpFunc("POST", url, args...)
}
