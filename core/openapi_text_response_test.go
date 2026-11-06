package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/openapi"
)

// A text/html version endpoint (dosco/graphjin#636) registers a closed
// table with a single value column and resolves to {"value": "<body>"}.
func TestOpenAPITextResponseRegistersAndResolvesValue(t *testing.T) {
	const version = "Magento/2.4 (Community)"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		_, _ = w.Write([]byte(version))
	}))
	defer srv.Close()

	loaded, err := openapi.Load(openapi.LoaderOptions{SpecsDir: t.TempDir()}, map[string]openapi.SpecConfig{"magento": {
		BaseURL: srv.URL,
		Document: `openapi: 3.0.3
info: {title: Magento, version: '1'}
paths:
  /magento_version:
    get:
      operationId: magentoGetVersion
      responses:
        "200":
          description: Magento release family and edition
          content:
            text/html:
              schema:
                type: string
`,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	gj := newTestEngineWithRealDBInfo(t, "public", "users")
	if err := gj.preRegisterOpenAPITables(loaded.Registry); err != nil {
		t.Fatal(err)
	}
	table, err := gj.primaryDB().dbinfo.GetTable("public", "magento_magento_get_version")
	if err != nil {
		t.Fatal(err)
	}
	if !table.StrictColumns || len(table.Columns) != 1 || table.Columns[0].Name != "value" || table.Columns[0].Type != "text" {
		t.Fatalf("table = strict:%v columns:%+v, want one strict text column named value", table.StrictColumns, table.Columns)
	}

	spec, _ := loaded.Registry.Get("magento")
	rt, err := openapi.NewSpecRuntime(spec, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	caller, ok := rt.Caller("magentoGetVersion")
	if !ok {
		t.Fatalf("operation skipped: %s", spec.Operations[0].SkipReason)
	}
	bridge := &openapiBridge{caller: caller, op: &spec.Operations[0], topLevel: true}
	body, err := bridge.Resolve(context.Background(), ResolverReq{Sel: &qcode.Select{}})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"value":"Magento/2.4 (Community)"}` {
		t.Fatalf("body = %s", body)
	}
}
