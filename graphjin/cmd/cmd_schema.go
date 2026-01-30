package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/cobra"
)

// cmdDBDiff shows the SQL diff between db.graphql and the database
func cmdDBDiff(cmd *cobra.Command, args []string) {
	setup(cpath)

	destructive, _ := cmd.Flags().GetBool("destructive")
	format, _ := cmd.Flags().GetString("format")

	// Multi-DB mode
	if isMultiDBMode() {
		results, err := computeSchemaDiffMulti(destructive)
		if err != nil {
			log.Fatalf("Failed to compute schema diff: %s", err)
		}

		// Check if any changes exist
		hasChanges := false
		for _, ops := range results {
			if len(ops) > 0 {
				hasChanges = true
				break
			}
		}

		if !hasChanges {
			log.Infof("No schema changes required")
			return
		}

		switch format {
		case "json":
			outputJSONMulti(results)
		default:
			outputSQLMulti(results, destructive)
		}
		return
	}

	// Single-DB mode
	initDB(true)

	ops, err := computeSchemaDiff(destructive)
	if err != nil {
		log.Fatalf("Failed to compute schema diff: %s", err)
	}

	if len(ops) == 0 {
		log.Infof("No schema changes required")
		return
	}

	switch format {
	case "json":
		outputJSON(ops)
	default:
		outputSQL(ops, destructive)
	}
}

// cmdDBSync applies the schema diff to the database
func cmdDBSync(cmd *cobra.Command, args []string) {
	setup(cpath)

	destructive, _ := cmd.Flags().GetBool("destructive")
	yes, _ := cmd.Flags().GetBool("yes")

	// Multi-DB mode
	if isMultiDBMode() {
		results, err := computeSchemaDiffMulti(destructive)
		if err != nil {
			log.Fatalf("Failed to compute schema diff: %s", err)
		}

		// Check if any changes exist
		hasChanges := false
		hasDestructive := false
		for _, ops := range results {
			if len(ops) > 0 {
				hasChanges = true
				for _, op := range ops {
					if op.Danger {
						hasDestructive = true
						break
					}
				}
			}
		}

		if !hasChanges {
			log.Infof("No schema changes required")
			return
		}

		// Show preview
		log.Infof("The following changes will be applied:")
		fmt.Println()
		outputSQLMulti(results, destructive)
		fmt.Println()

		if hasDestructive {
			log.Warnf("WARNING: This operation includes DESTRUCTIVE changes (DROP statements)")
		}

		// Confirm unless --yes is provided
		if !yes {
			fmt.Print("Do you want to apply these changes? Type 'yes' to confirm: ")
			reader := bufio.NewReader(os.Stdin)
			response, err := reader.ReadString('\n')
			if err != nil {
				log.Fatalf("Error reading input: %s", err)
			}
			response = strings.TrimSpace(response)

			if strings.ToLower(response) != "yes" {
				log.Infof("Aborted")
				return
			}
		}

		// Apply changes
		if err := applyChangesMulti(results); err != nil {
			log.Fatalf("Failed to apply schema changes: %s", err)
		}

		log.Infof("Schema changes applied successfully")
		return
	}

	// Single-DB mode
	initDB(true)

	ops, err := computeSchemaDiff(destructive)
	if err != nil {
		log.Fatalf("Failed to compute schema diff: %s", err)
	}

	if len(ops) == 0 {
		log.Infof("No schema changes required")
		return
	}

	// Show preview
	log.Infof("The following changes will be applied:")
	fmt.Println()
	outputSQL(ops, destructive)
	fmt.Println()

	// Check for destructive operations
	hasDestructive := false
	for _, op := range ops {
		if op.Danger {
			hasDestructive = true
			break
		}
	}

	if hasDestructive {
		log.Warnf("WARNING: This operation includes DESTRUCTIVE changes (DROP statements)")
	}

	// Confirm unless --yes is provided
	if !yes {
		fmt.Print("Do you want to apply these changes? Type 'yes' to confirm: ")
		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			log.Fatalf("Error reading input: %s", err)
		}
		response = strings.TrimSpace(response)

		if strings.ToLower(response) != "yes" {
			log.Infof("Aborted")
			return
		}
	}

	// Apply changes in a transaction
	sqls := core.GenerateDiffSQL(ops)
	if err := applyChanges(sqls); err != nil {
		log.Fatalf("Failed to apply schema changes: %s", err)
	}

	log.Infof("Schema changes applied successfully")
}

// computeSchemaDiff computes the diff between db.graphql and the database
func computeSchemaDiff(destructive bool) ([]core.SchemaOperation, error) {
	// Read db.graphql from config path
	schemaPath := filepath.Join(cpath, "db.graphql")
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read db.graphql: %w", err)
	}

	// Compute diff using core package
	opts := core.DiffOptions{
		Destructive: destructive,
	}

	ops, err := core.SchemaDiff(db, conf.DB.Type, schemaBytes, conf.Blocklist, opts)
	if err != nil {
		return nil, err
	}

	return ops, nil
}

// outputSQL prints the operations as SQL statements
func outputSQL(ops []core.SchemaOperation, showDestructive bool) {
	for _, op := range ops {
		if op.Danger && !showDestructive {
			continue
		}

		prefix := ""
		if op.Danger {
			prefix = "-- DESTRUCTIVE: "
		}

		switch op.Type {
		case "create_table":
			fmt.Printf("%s-- Create table: %s\n", prefix, op.Table)
		case "add_column":
			fmt.Printf("%s-- Add column: %s.%s\n", prefix, op.Table, op.Column)
		case "drop_table":
			fmt.Printf("%s-- Drop table: %s\n", prefix, op.Table)
		case "drop_column":
			fmt.Printf("%s-- Drop column: %s.%s\n", prefix, op.Table, op.Column)
		case "add_index":
			fmt.Printf("%s-- Add index on: %s.%s\n", prefix, op.Table, op.Column)
		case "add_constraint":
			fmt.Printf("%s-- Add constraint on: %s.%s\n", prefix, op.Table, op.Column)
		}

		fmt.Println(op.SQL)
		fmt.Println()
	}
}

// outputJSON prints the operations as JSON
func outputJSON(ops []core.SchemaOperation) {
	type jsonOp struct {
		Type        string `json:"type"`
		Table       string `json:"table"`
		Column      string `json:"column,omitempty"`
		SQL         string `json:"sql"`
		Destructive bool   `json:"destructive,omitempty"`
	}

	var jsonOps []jsonOp
	for _, op := range ops {
		jsonOps = append(jsonOps, jsonOp{
			Type:        op.Type,
			Table:       op.Table,
			Column:      op.Column,
			SQL:         op.SQL,
			Destructive: op.Danger,
		})
	}

	output, err := json.MarshalIndent(jsonOps, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal JSON: %s", err)
	}
	fmt.Println(string(output))
}

// applyChanges executes the SQL statements in a transaction
func applyChanges(sqls []string) error {
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	for _, sql := range sqls {
		if _, err := tx.ExecContext(ctx, sql); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Warnf("Rollback failed: %s", rbErr)
			}
			return fmt.Errorf("failed to execute SQL: %s\nError: %w", sql, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// isMultiDBMode checks if multi-database mode is configured
func isMultiDBMode() bool {
	return len(conf.Databases) > 0
}

// openMultiDBConnections opens connections to all configured databases
func openMultiDBConnections() (map[string]*sql.DB, error) {
	connections := make(map[string]*sql.DB)
	fs := core.NewOsFS(cpath)

	for name, dbConf := range conf.Databases {
		// Create a temporary config for this database
		tempConf := &serv.Config{}
		*tempConf = *conf
		tempConf.DB = serv.Database{
			Type:       dbConf.Type,
			Host:       dbConf.Host,
			Port:       uint16(dbConf.Port),
			User:       dbConf.User,
			Password:   dbConf.Password,
			DBName:     dbConf.DBName,
			ConnString: dbConf.ConnString,
		}

		conn, err := serv.NewDB(tempConf, true, log, fs)
		if err != nil {
			// Close any already opened connections
			for _, c := range connections {
				c.Close() //nolint:errcheck
			}
			return nil, fmt.Errorf("failed to connect to database '%s': %w", name, err)
		}
		connections[name] = conn
	}

	return connections, nil
}

// computeSchemaDiffMulti computes schema diff for all databases in multi-DB mode
func computeSchemaDiffMulti(destructive bool) (map[string][]core.SchemaOperation, error) {
	// Read db.graphql from config path
	schemaPath := filepath.Join(cpath, "db.graphql")
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read db.graphql: %w", err)
	}

	// Open connections to all databases
	connections, err := openMultiDBConnections()
	if err != nil {
		return nil, err
	}
	defer func() {
		for _, conn := range connections {
			conn.Close() //nolint:errcheck
		}
	}()

	// Build dbTypes map from config
	dbTypes := make(map[string]string)
	for name, dbConf := range conf.Databases {
		dbTypes[name] = dbConf.Type
	}

	// Compute diff using core package
	opts := core.DiffOptions{
		Destructive: destructive,
	}

	return core.SchemaDiffMultiDB(connections, dbTypes, schemaBytes, conf.Blocklist, opts)
}

// outputSQLMulti prints the operations for all databases as SQL statements
func outputSQLMulti(results map[string][]core.SchemaOperation, showDestructive bool) {
	for dbName, ops := range results {
		if len(ops) == 0 {
			continue
		}

		fmt.Printf("-- Database: %s\n", dbName)
		fmt.Println("-- " + strings.Repeat("-", 40))
		outputSQL(ops, showDestructive)
		fmt.Println()
	}
}

// outputJSONMulti prints the operations for all databases as JSON
func outputJSONMulti(results map[string][]core.SchemaOperation) {
	type jsonOp struct {
		Database    string `json:"database"`
		Type        string `json:"type"`
		Table       string `json:"table"`
		Column      string `json:"column,omitempty"`
		SQL         string `json:"sql"`
		Destructive bool   `json:"destructive,omitempty"`
	}

	var jsonOps []jsonOp
	for dbName, ops := range results {
		for _, op := range ops {
			jsonOps = append(jsonOps, jsonOp{
				Database:    dbName,
				Type:        op.Type,
				Table:       op.Table,
				Column:      op.Column,
				SQL:         op.SQL,
				Destructive: op.Danger,
			})
		}
	}

	output, err := json.MarshalIndent(jsonOps, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal JSON: %s", err)
	}
	fmt.Println(string(output))
}

// applyChangesMulti applies schema changes to all databases
func applyChangesMulti(results map[string][]core.SchemaOperation) error {
	// Open connections to all databases
	connections, err := openMultiDBConnections()
	if err != nil {
		return err
	}
	defer func() {
		for _, conn := range connections {
			conn.Close() //nolint:errcheck
		}
	}()

	ctx := context.Background()
	totalChanges := 0

	for dbName, ops := range results {
		if len(ops) == 0 {
			continue
		}

		sqls := core.GenerateDiffSQL(ops)
		conn := connections[dbName]

		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to begin transaction for %s: %w", dbName, err)
		}

		var failed bool
		for _, sqlStmt := range sqls {
			if _, err := tx.ExecContext(ctx, sqlStmt); err != nil {
				if rbErr := tx.Rollback(); rbErr != nil {
					log.Warnf("Rollback failed for %s: %s", dbName, rbErr)
				}
				return fmt.Errorf("failed to apply schema to %s: %w", dbName, err)
			}
		}

		if !failed {
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("failed to commit changes to %s: %w", dbName, err)
			}
			log.Infof("[%s] Schema changes applied (%d changes)", dbName, len(ops))
			totalChanges += len(ops)
		}
	}

	log.Infof("Total schema changes applied: %d", totalChanges)
	return nil
}
