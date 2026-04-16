package main

import "github.com/spf13/cobra"

// mcpSchemaCmd wires `graphjin cli schema ...` — schema introspection tools.
// The cobra command is named `schema`; the *file* is mcp_introspect.go to
// avoid visual collision with cmd_schema.go (DB DDL under `db`).
func mcpSchemaCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "schema",
		Short: "Explore schema, relationships, and join paths on a running GraphJin server",
	}
	c.AddCommand(mcpSchemaTablesCmd())
	c.AddCommand(mcpSchemaDescribeCmd())
	c.AddCommand(mcpSchemaExploreCmd())
	c.AddCommand(mcpSchemaPathCmd())
	return c
}

var mcpSchemaDatabase string

func mcpSchemaTablesCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "tables",
		Short: "List all database tables (MCP: list_tables)",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			runToolCmd(cmd, "list_tables", map[string]any{
				"database": mcpSchemaDatabase,
			})
		},
	}
	c.Flags().StringVar(&mcpSchemaDatabase, "database", "", "Filter by database name (optional)")
	return c
}

var mcpSchemaDescribeDatabase string

func mcpSchemaDescribeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "describe <table>",
		Short: "Show a table's columns, relationships, and aggregations (MCP: describe_table)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			runToolCmd(cmd, "describe_table", map[string]any{
				"table":    args[0],
				"database": mcpSchemaDescribeDatabase,
			})
		},
	}
	c.Flags().StringVar(&mcpSchemaDescribeDatabase, "database", "", "Database name when the table is ambiguous across DBs")
	return c
}

var (
	mcpSchemaExploreDatabase string
	mcpSchemaExploreDepth    int
)

func mcpSchemaExploreCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "explore <table>",
		Short: "Walk the relationship graph around a table (MCP: explore_relationships)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			args0 := map[string]any{
				"table":    args[0],
				"database": mcpSchemaExploreDatabase,
			}
			if mcpSchemaExploreDepth > 0 {
				args0["depth"] = mcpSchemaExploreDepth
			}
			runToolCmd(cmd, "explore_relationships", args0)
		},
	}
	c.Flags().StringVar(&mcpSchemaExploreDatabase, "database", "", "Database name when the table is ambiguous across DBs")
	c.Flags().IntVar(&mcpSchemaExploreDepth, "depth", 0, "Relationship depth to walk (1-3; server default when 0)")
	return c
}

var mcpSchemaPathDatabase string

func mcpSchemaPathCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "path <from> <to>",
		Short: "Find the shortest join path between two tables (MCP: find_path)",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			runToolCmd(cmd, "find_path", map[string]any{
				"from_table": args[0],
				"to_table":   args[1],
				"database":   mcpSchemaPathDatabase,
			})
		},
	}
	c.Flags().StringVar(&mcpSchemaPathDatabase, "database", "", "Database name when tables are ambiguous across DBs")
	return c
}
