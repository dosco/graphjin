package tests_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/openapi"
)

// Example_queryWithOpenAPITextResponse pins dosco/graphjin#636: a GET whose
// 200 response is declared as a text/html string (the Magento version
// endpoint) is active, exposes a single value field, and returns the exact
// body as data.
func Example_queryWithOpenAPITextResponse() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, "Magento/2.4 (Community)") //nolint:errcheck
	}))
	defer server.Close()

	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		Sources: []core.SourceConfig{
			{Name: core.DefaultDBName, Kind: "database", Type: dbType, Default: true, Access: core.SourceAccessConfig{
				Read: core.AccessModeAuthenticated,
			}},
			{
				Name: "magento_operator",
				Kind: "api",
				Specs: map[string]openapi.SpecConfig{
					"magento_operator_v2": {
						BaseURL: server.URL,
						Document: `openapi: 3.0.3
info: { title: Magento, version: '1' }
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
					},
				},
			},
		},
	})

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer gj.Close()

	gql := `query MagentoVersion {
		magento_operator_v2_magento_get_version {
			value
		}
	}`

	res, err := gj.GraphQL(sourceModeIntegrationUserContext(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"magento_operator_v2_magento_get_version":{"value":"Magento/2.4 (Community)"}}
}
