package tests_test

import (
	"fmt"
	"testing"
)

func TestDebugSchemaTypes(t *testing.T) {
	if db == nil {
		t.Skip("DB not initialized")
	}

	// Query to see all check constraints related to json_valid
	query := `
		SELECT 
			constraint_schema,
			constraint_name,
			check_clause
		FROM information_schema.check_constraints 
		WHERE constraint_schema = DATABASE()
			AND LOWER(check_clause) LIKE '%json_valid%'
	`
	
	rows, err := db.Query(query)
	if err != nil {
		t.Logf("Query failed: %v", err)
		return
	}
	defer rows.Close()

	fmt.Println("\n=== JSON Valid Check Constraints ===")
	for rows.Next() {
		var schema, name, clause string
		if err := rows.Scan(&schema, &name, &clause); err != nil {
			t.Logf("Scan error: %v", err)
			continue
		}
		fmt.Printf("Schema: %-10s Name: %-25s Clause: %s\n", schema, name, clause)
	}
	fmt.Println("====================================")
}
