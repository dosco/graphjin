package tests_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3"
)

const productByIDWorkflow = `
function main(input) {
  return gj.tools.executeGraphql({
    query: "query($id: Int!) { products(id: $id) { id name } }",
    variables: { id: input.id }
  });
}
`

func TestWorkflowRESTExecuteGraphQLWithPostVariables(t *testing.T) {
	h := newWorkflowHandler(t, "product_by_id", productByIDWorkflow)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/product_by_id", bytes.NewBufferString(`{"id":2}`))
	req.Header.Set("Content-Type", "application/json")

	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	assertWorkflowProductResponse(t, res, 2)
}

func TestWorkflowRESTExecuteGraphQLWithGetVariables(t *testing.T) {
	h := newWorkflowHandler(t, "product_by_id", productByIDWorkflow)

	vars := url.QueryEscape(`{"id":3}`)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workflows/product_by_id?variables="+vars, nil)

	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	assertWorkflowProductResponse(t, res, 3)
}

func newWorkflowHandler(t *testing.T, workflowName, workflowJS string) http.Handler {
	t.Helper()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}

	workflowFile := filepath.Join(dir, "workflows", workflowName+".js")
	if err := os.WriteFile(workflowFile, []byte(workflowJS), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	coreConf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
	})

	svcConf := &serv.Config{Core: *coreConf}
	svcConf.MCP.AllowRawQueries = true

	gjs, err := serv.NewGraphJinService(
		svcConf,
		serv.OptionSetDB(db),
		serv.OptionSetFS(core.NewOsFS(dir)),
	)
	if err != nil {
		t.Fatalf("new graphjin service: %v", err)
	}

	return gjs.Workflows(nil)
}

func assertWorkflowProductResponse(t *testing.T, res *httptest.ResponseRecorder, wantID int) {
	t.Helper()

	if res.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", res.Code, res.Body.String())
	}

	var out struct {
		Data struct {
			Data struct {
				Products struct {
					ID int `json:"id"`
				} `json:"products"`
			} `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		} `json:"data"`
		Errors []string `json:"errors"`
	}

	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode workflow response: %v", err)
	}

	if len(out.Errors) != 0 {
		t.Fatalf("unexpected endpoint errors: %v", out.Errors)
	}
	if len(out.Data.Errors) != 0 {
		t.Fatalf("unexpected graphql errors: %+v", out.Data.Errors)
	}
	if out.Data.Data.Products.ID != wantID {
		t.Fatalf("expected product id=%d, got %d", wantID, out.Data.Data.Products.ID)
	}
}
