package core

import (
	"strings"
	"testing"
)

func TestPrettify(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		dbType   string
		contains []string // Substrings we expect to see (e.g. newlines before keywords)
	}{
		{
			name:   "Basic Select",
			query:  "SELECT * FROM users WHERE id = 1",
			dbType: "postgres",
			contains: []string{
				"SELECT * ",
				"\nFROM users ",
				"\nWHERE id = 1",
			},
		},
		{
			name:   "Postgres Quoted Identifiers",
			query:  `SELECT "id", "name" FROM "users" WHERE "name" = 'John'`,
			dbType: "postgres",
			contains: []string{
				`SELECT "id", "name" `,
				`\nFROM "users" `,
				`\nWHERE "name" = 'John'`,
			},
		},
		{
			name:   "MySQL Backtick Identifiers",
			query:  "SELECT `id`, `name` FROM `users` WHERE `name` = 'John'",
			dbType: "mysql",
			contains: []string{
				"SELECT `id`, `name` ",
				"\nFROM `users` ",
				"\nWHERE `name` = 'John'",
			},
		},
		{
			name:   "String Literal Protection",
			query:  "SELECT 'SELECT' FROM users",
			dbType: "postgres",
			contains: []string{
				"SELECT 'SELECT' ",
				"\nFROM users",
			},
		},
		{
			name:   "Complex Query",
			query:  "SELECT u.id, p.name FROM users u JOIN products p ON u.id = p.user_id WHERE p.price > 10 ORDER BY p.price DESC LIMIT 5",
			dbType: "postgres",
			contains: []string{
				"SELECT u.id, p.name ",
				"\nFROM users u ",
				"\nJOIN products p ", // We handle JOIN separately? Yes in list
				"ON u.id = p.user_id ",
				"\nWHERE p.price > 10 ",
				"\nORDER BY p.price DESC ",
				"\nLIMIT 5",
			},
		},
		{
			name: "Nested String Escaping Standard",
			query: "SELECT 'It''s me' FROM users",
			dbType: "postgres",
			contains: []string{
				"SELECT 'It''s me' ",
				"\nFROM users",
			},
		},
        {
            name: "MySQL Escaped Backslash",
            query: "SELECT 'foo\\'bar' FROM users",
            dbType: "mysql",
            contains: []string{
                "SELECT 'foo\\'bar' ",
                "\nFROM users",
            },
        },
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := prettify(tt.query, tt.dbType)
			for _, c := range tt.contains {
                // We use strings.Contains, but we need to match carefully because we insert newlines
                // The expected strings in `contains` assume \n is present.
                // Replace \n with actual newline for check if needed, or just check exact string
                expected := strings.ReplaceAll(c, "\\n", "\n")
				if !strings.Contains(got, expected) {
					t.Errorf("prettify() expected to contain %q, but it didn't.\nResult:\n%q", expected, got)
				}
			}
            
            // Safety check: Is the semantic content (ignoring whitespace) the same?
            // This is hard to check perfectly because we added newlines. 
            // But if we strip all whitespace from both, they should match?
            // No, because we might have added space between keywords.
            // Let's just trust our contains checks + manual inspection for now.
		})
	}
}
