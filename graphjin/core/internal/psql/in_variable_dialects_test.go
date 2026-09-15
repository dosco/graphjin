package psql_test

import (
	"strings"
	"testing"
)

// A JSON array variable used with in or nin must compare text columns as JSON
// strings, and nin must negate the match. MySQL once cast text columns to JSON,
// which fails for plain text, and rendered nin exactly like in.
func TestInVariableRendersTextAndNumberColumns(t *testing.T) {
	cases := []struct {
		db, gql, want string
	}{
		{"mysql", `query { users(where: { email: { in: $v } }) { id } }`,
			"WHERE JSON_CONTAINS(?, JSON_QUOTE(`users`.`email`), '$')"},
		{"mysql", `query { users(where: { email: { nin: $v } }) { id } }`,
			"WHERE NOT JSON_CONTAINS(?, JSON_QUOTE(`users`.`email`), '$')"},
		{"mysql", `query { users(where: { id: { in: $v } }) { id } }`,
			"WHERE JSON_CONTAINS(?, CAST(`users`.`id` AS JSON), '$')"},
		{"mysql", `query { users(where: { id: { nin: $v } }) { id } }`,
			"WHERE NOT JSON_CONTAINS(?, CAST(`users`.`id` AS JSON), '$')"},
		{"mariadb", `query { users(where: { email: { in: $v } }) { id } }`,
			"WHERE JSON_CONTAINS(?, JSON_QUOTE(`users_0`.`email`))"},
		{"mariadb", `query { users(where: { email: { nin: $v } }) { id } }`,
			"WHERE NOT JSON_CONTAINS(?, JSON_QUOTE(`users_0`.`email`))"},
		{"mariadb", `query { users(where: { id: { in: $v } }) { id } }`,
			"WHERE JSON_CONTAINS(?, `users_0`.`id`)"},
	}
	for _, tc := range cases {
		t.Run(tc.db+" "+tc.gql, func(t *testing.T) {
			qc, pc := newDialectCompilers(t, tc.db)
			sql := compileWith(t, qc, pc, tc.gql)
			if !strings.Contains(sql, tc.want) {
				t.Fatalf("SQL does not contain %q:\n%s", tc.want, sql)
			}
		})
	}
}
