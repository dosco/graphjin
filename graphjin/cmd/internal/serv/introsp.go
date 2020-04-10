package serv

import "net/http"

//nolint: errcheck
func introspect(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{
		"data": {
			"__schema": {
				"queryType": {
					"name": "Query"
				},
				"mutationType": null,
				"subscriptionType": null
			}
		},
		"extensions":{  
			"tracing":{  
				"version":1,
				"startTime":"2019-06-04T19:53:31.093Z",
				"endTime":"2019-06-04T19:53:31.108Z",
				"duration":15219720,
				"execution": {
					"resolvers": [{
						"path": ["__schema"],
						"parentType":	"Query",
						"fieldName": "__schema",
						"returnType":	"__Schema!",
						"startOffset": 50950,
						"duration": 17187
					}]
				}
			}
		}
	}`))
}
