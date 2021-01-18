package core_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dosco/graphjin/core"
	"github.com/stretchr/testify/assert"
)

func TestQuery(t *testing.T) {
	t.Run("queryWithVariableLimit", queryWithVariableLimit)
}

func queryWithVariableLimit(t *testing.T) {
	gql := `query {
		products(limit: $limit) {
			id
		}
	}`

	vars := json.RawMessage(`{
		"limit": 10
	}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Error(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars)

	switch dbType {
	case "mysql":
		assert.NotNil(t, err)
	default:
		exp := `{"products": [{"id": 1}, {"id": 2}, {"id": 3}, {"id": 4}, {"id": 5}, {"id": 6}, {"id": 7}, {"id": 8}, {"id": 9}, {"id": 10}]}`
		got := string(res.Data)
		assert.Equal(t, got, exp, "should equal")
	}
}
