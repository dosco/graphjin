package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/dosco/graphjin/core/v3"
)

func main() {
	// Create a temporary directory structure for testing
	tempDir := "/tmp/graphjin-openapi-test"
	queryDir := filepath.Join(tempDir, "queries")

	// Clean up any existing test directory
	os.RemoveAll(tempDir)

	// Create directory structure
	if err := os.MkdirAll(queryDir, 0755); err != nil {
		log.Fatal("Failed to create test directory:", err)
	}

	// Create some test queries
	testQueries := map[string]string{
		"getUsers.gql": `query getUsers($limit: Int = 10) {
  users(limit: $limit, where: { id: { gt: 0 } }) {
    id
    full_name
    email
    created_at
    products {
      id
      name
      price
    }
  }
}`,
		"createUser.gql": `mutation createUser($name: String!, $email: String!) {
  users(insert: { full_name: $name, email: $email }) {
    id
    full_name
    email
    created_at
  }
}`,
		"updateUser.gql": `mutation updateUser($id: Int!, $name: String) {
  users(where: { id: { eq: $id } }, update: { full_name: $name }) {
    id
    full_name
    email
    updated_at
  }
}`,
		"deleteUser.gql": `mutation deleteUser($id: Int!) {
  users(where: { id: { eq: $id } }, delete: true) {
    id
  }
}`,
		"getProducts.gql": `query getProducts($search: String, $limit: Int = 20) {
  products(
    limit: $limit,
    order_by: { price: desc },
    where: { 
      name: { ilike: $search }
    }
  ) {
    id
    name
    price
    description
    created_at
    user {
      id
      full_name
    }
  }
}`,
	}

	// Write test queries to files
	for filename, query := range testQueries {
		queryPath := filepath.Join(queryDir, filename)
		if err := os.WriteFile(queryPath, []byte(query), 0644); err != nil {
			log.Fatal("Failed to write query file:", err)
		}
	}

	// Create a minimal GraphJin configuration
	config := &core.Config{
		DB: core.DBConfig{
			Type: "postgres",                     // This would normally be an actual database
			DSN:  "postgres://localhost/test_db", // Mock DSN
		},
		Debug: true,
	}

	// Initialize GraphJin (this will fail without a real database, but we can still test the basic structure)
	gj, err := core.NewGraphJin(config, tempDir)
	if err != nil {
		log.Printf("Expected error initializing GraphJin (no database): %v", err)
		log.Println("Continuing with basic structural testing...")

		// For testing purposes, let's just print what we would generate
		log.Println("\nTest queries created:")
		for filename := range testQueries {
			log.Printf("- %s", filename)
		}

		log.Println("\nExpected OpenAPI structure would include:")
		log.Println("- /getUsers (GET, POST) - Query operation")
		log.Println("- /createUser (POST) - Insert mutation")
		log.Println("- /updateUser (PUT, POST) - Update mutation")
		log.Println("- /deleteUser (DELETE, POST) - Delete mutation")
		log.Println("- /getProducts (GET, POST) - Query operation")

		log.Println("\nEach endpoint would have:")
		log.Println("- Parameters derived from GraphQL variables")
		log.Println("- Response schemas derived from GraphQL selections")
		log.Println("- Appropriate HTTP methods based on operation type")

		return
	}

	// Generate OpenAPI specification
	log.Println("Generating OpenAPI specification...")
	spec, err := gj.GetOpenAPISpec()
	if err != nil {
		log.Fatal("Failed to generate OpenAPI spec:", err)
	}

	// Parse and pretty-print the JSON
	var prettySpec interface{}
	if err := json.Unmarshal(spec, &prettySpec); err != nil {
		log.Fatal("Failed to parse OpenAPI spec JSON:", err)
	}

	prettyJSON, err := json.MarshalIndent(prettySpec, "", "  ")
	if err != nil {
		log.Fatal("Failed to format OpenAPI spec:", err)
	}

	fmt.Println("Generated OpenAPI 3.0 Specification:")
	fmt.Println(string(prettyJSON))

	// Clean up
	os.RemoveAll(tempDir)
}
