package js

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/dop251/goja"
)

func (s *JScript) httpFunc(method string, url goja.Value, args ...goja.Value) goja.Value {
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
	return s.vm.ToValue(string(buf))
}

func (s *JScript) httpGetFunc(url goja.Value, args ...goja.Value) goja.Value {
	return s.httpFunc("GET", url, args...)
}

func (s *JScript) httpPostFunc(url goja.Value, args ...goja.Value) goja.Value {
	return s.httpFunc("POST", url, args...)
}
