package migrate

import (
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func MigrateSchema(current, expected sdata.DBInfo) []string {
	if current.Type != expected.Type {
		fmt.Println("Database type mismatch.")
		return nil
	}

	operations := []string{}
	lockName := "__gj_migration_lock"

	// Start Transaction and Acquire Lock
	switch expected.Type {
	case "mysql":
		operations = append(operations, "START TRANSACTION;")
		operations = append(operations, fmt.Sprintf("SELECT GET_LOCK('%s', 10);", lockName)) // Wait up to 10 seconds to acquire lock
	case "postgresql":
		operations = append(operations, "BEGIN;")
		operations = append(operations, fmt.Sprintf("SELECT pg_advisory_xact_lock(%d);", hashStringToInt(lockName)))
	}

	// Locking mechanism (assuming schema_migrations table for the sake of simplicity)
	switch expected.Type {
	case "mysql":
		operations = append(operations, "LOCK TABLES schema_migrations WRITE;")
	case "postgresql":
		operations = append(operations, "LOCK TABLE schema_migrations IN ACCESS EXCLUSIVE MODE;")
	}

	for _, expTable := range expected.Tables {
		var currTable *sdata.DBTable
		for _, ct := range current.Tables {
			if ct.Name == expTable.Name {
				currTable = &ct
				break
			}
		}

		if currTable == nil {
			// Create table
			operations = append(operations, CreateTableQuery(expTable, expected.Type))
			continue
		}

		// If table is found, compare columns
		for _, expCol := range expTable.Columns {
			var currCol *sdata.DBColumn
			for _, cc := range currTable.Columns {
				if cc.Name == expCol.Name {
					currCol = &cc
					break
				}
			}

			if currCol == nil {
				// Add column
				operations = append(operations, AddColumnQuery(expTable, expCol, expected.Type))
			} else {
				// Rename column if needed
				if currCol.Name != expCol.Name {
					operations = append(operations, RenameColumnQuery(expTable, *currCol, expCol, expected.Type))
				}
			}
		}
	}

	// Commit Transaction
	switch expected.Type {
	case "mysql":
		operations = append(operations, "COMMIT;")
		operations = append(operations, fmt.Sprintf("SELECT RELEASE_LOCK('%s');", lockName))
	default:
		operations = append(operations, "COMMIT;") // Advisory locks in PostgreSQL are automatically released at the end of the transaction
	}

	return operations
}

func CreateTableQuery(table sdata.DBTable, dbType string) string {
	var columns []string
	for _, col := range table.Columns {
		columns = append(columns, fmt.Sprintf("%s %s", col.Name, col.Type))
	}
	return fmt.Sprintf("CREATE TABLE %s (%s);", table.Name, strings.Join(columns, ", "))
}

func AddColumnQuery(table sdata.DBTable, column sdata.DBColumn, dbType string) string {
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;", table.Name, column.Name, column.Type)
}

func RenameColumnQuery(table sdata.DBTable, currentCol sdata.DBColumn, expectedCol sdata.DBColumn, dbType string) string {
	switch dbType {
	case "mysql":
		return fmt.Sprintf("ALTER TABLE %s CHANGE COLUMN %s %s %s;", table.Name, currentCol.Name, expectedCol.Name, expectedCol.Type)
	case "postgresql":
		return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s;", table.Name, currentCol.Name, expectedCol.Name)
	default:
		return ""
	}
}

// Hash a string to an integer, suitable for use with PostgreSQL's pg_advisory_xact_lock.
// Using FNV-1a hashing as an example, but you could use another approach if you prefer.
func hashStringToInt(s string) int {
	h := fnv.New32a()
	h.Write([]byte(s))
	return int(h.Sum32())
}

// Usage:
// current := DBInfo{...}
// expected := DBInfo{...}
// ops := MigrateSchema(current, expected)
// fmt.Println(strings.Join(ops, "\n"))
