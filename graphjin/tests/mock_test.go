package tests_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/assert"
)

func TestMockDB(t *testing.T) {
	// Create a temp directory
	dir, err := os.MkdirTemp("", "graphjin_mock_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir) //nolint:errcheck

	// Copy test-db.graphql to db.graphql in temp dir
	dbGraphql, err := os.ReadFile("test-db.graphql")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/db.graphql", dbGraphql, 0644); err != nil {
		t.Fatal(err)
	}

	conf := core.Config{
		MockDB:       true,
		EnableSchema: true, 
	}

	fs := core.NewOsFS(dir)

	gj, err := core.NewGraphJinWithFS(&conf, nil, fs)
	if err != nil {
		t.Fatal(err)
	}

	gql := `query mockDBTest {
		users {
			id
			full_name
			email
            products {
                name
                price
            }
		}
	}`

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		t.Fatal(err)
	}



	// Verify structure
	var data struct {
		Users []struct {
			ID       int
			FullName string `json:"full_name"`
			Email    string
            Products []struct {
                Name string
                Price float64
            }
		} `json:"users"`
	}

	if err := json.Unmarshal(res.Data, &data); err != nil {
		t.Fatal(err)
	}

	assert.NotEmpty(t, data.Users)
	assert.Greater(t, data.Users[0].ID, 0)
	assert.NotEmpty(t, data.Users[0].FullName)
    
    // Check nested
    if len(data.Users[0].Products) > 0 {
        assert.NotEmpty(t, data.Users[0].Products[0].Name)
    }
}
