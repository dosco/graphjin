package main

import (
	"github.com/spf13/cobra"
)

// dbCmd creates the db command
func dbCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "db",
		Short: "Database schema management commands",
	}

	// Diff command - show schema differences
	diffCmd := &cobra.Command{
		Use:   "diff",
		Short: "Show SQL diff between GraphJin DDL and database",
		Long: `Compare the desired schema (db.ddl or schema-ddl/<source>.ddl) against the actual database schema
and output the SQL statements needed to bring the database in sync.

By default, only safe (additive) operations are shown:
- CREATE TABLE
- ADD COLUMN
- ADD CONSTRAINT
- CREATE INDEX

Use --destructive to also show DROP operations.`,
		Run: cmdDBDiff,
	}
	diffCmd.Flags().Bool("destructive", false, "Include DROP TABLE/COLUMN statements")
	diffCmd.Flags().String("format", "sql", "Output format: sql or json")
	c.AddCommand(diffCmd)

	// Generate command - generate GraphJin DDL from database
	generateCmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate GraphJin DDL from current database schema",
		Long: `Introspect the current database and generate GraphJin DDL.

This command connects to the configured database, discovers all tables and columns,
and writes schema DDL that can be used for schema management.

The generated schema includes:
- All tables as GraphQL types
- All columns with appropriate GraphQL types
- Primary keys (@id), unique constraints (@unique), and relationships (@relation)
- Database functions (if any)`,
		Run: cmdDBGenerate,
	}
	generateCmd.Flags().StringP("output", "o", "", "Output file path (default: <config-path>/db.ddl or schema-ddl/<source>.ddl in source mode)")
	c.AddCommand(generateCmd)

	// Sync command - apply schema changes
	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Apply schema diff to database",
		Long: `Apply the schema changes needed to bring the database in sync with GraphJin DDL.

This command will:
1. Show the SQL statements to be executed
2. Prompt for confirmation (unless --yes is used)
3. Execute the statements in a transaction

By default, only safe (additive) operations are applied.
Use --destructive to also apply DROP operations.`,
		Run: cmdDBSync,
	}
	syncCmd.Flags().Bool("destructive", false, "Allow DROP TABLE/COLUMN operations")
	syncCmd.Flags().Bool("yes", false, "Skip confirmation prompt")
	c.AddCommand(syncCmd)

	// Seed command
	seedCmd := &cobra.Command{
		Use:   "seed",
		Short: "Run the seed script to seed the database",
		Run:   cmdDBSeed,
	}
	c.AddCommand(seedCmd)

	// Setup command - now just seeds without migrations
	setupCmd := &cobra.Command{
		Use:   "setup",
		Short: "Setup database (sync schema and seed)",
		Long: `This command will sync the schema from GraphJin DDL and run the seed script.
Use 'db diff' first to preview changes.`,
		Run: cmdDBSetup,
	}
	setupCmd.Flags().Bool("yes", false, "Skip confirmation prompt for sync")
	c.AddCommand(setupCmd)

	return c
}

// cmdDBSetup sets up the database by syncing schema and seeding
func cmdDBSetup(cmd *cobra.Command, args []string) {
	setup(cpath)

	// First sync the schema
	cmdDBSync(cmd, []string{})

	// Then seed the database
	cmdDBSeed(cmd, []string{})
}
