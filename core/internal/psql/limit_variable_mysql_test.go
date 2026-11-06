package psql_test

import (
	"strings"
	"testing"
)

// Issue #445: MySQL rejected limit: $limit and MariaDB silently used the
// default limit. Both now bind a placeholder capped at the configured limit.
func TestMySQLFamilyLimitAndOffsetVariables(t *testing.T) {
	for _, db := range []string{"mysql", "mariadb"} {
		t.Run(db, func(t *testing.T) {
			qc, pc := newDialectCompilers(t, db)
			reqQC, err := qc.Compile([]byte(`query { products(limit: $limit, offset: $off) { id } }`), nil, "user", "")
			if err != nil {
				t.Fatal(err)
			}
			md, sqlBytes, err := pc.CompileEx(reqQC)
			if err != nil {
				t.Fatal(err)
			}
			if sql := string(sqlBytes); !strings.Contains(sql, "LIMIT ?, ?") {
				t.Fatalf("expected LIMIT ?, ? in:\n%s", sql)
			}
			var limitMax, offsetMax int32 = -1, -1
			for _, p := range md.Params() {
				switch p.Name {
				case "limit":
					limitMax = p.Max
				case "off":
					offsetMax = p.Max
				}
			}
			if limitMax != reqQC.Selects[0].Paging.Limit || limitMax <= 0 {
				t.Fatalf("limit param cap = %d, want the configured limit %d", limitMax, reqQC.Selects[0].Paging.Limit)
			}
			if offsetMax != 0 {
				t.Fatalf("offset param must not be capped, got %d", offsetMax)
			}
		})
	}
}
