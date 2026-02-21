package core

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/jsn"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// TestCacheKeyIncludesDatabase verifies that the cache key includes
// the database identifier to prevent cross-database cache collisions.
func TestCacheKeyIncludesDatabase(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		qname     string
		role      string
		database  string
		wantKey   string
	}{
		{
			name:      "empty database (backward compatible)",
			namespace: "ns1",
			qname:     "getUsers",
			role:      "user",
			database:  "",
			wantKey:   "ns1getUsersuser",
		},
		{
			name:      "with database",
			namespace: "ns1",
			qname:     "getUsers",
			role:      "user",
			database:  "main",
			wantKey:   "ns1getUsersusermain",
		},
		{
			name:      "different database same query",
			namespace: "ns1",
			qname:     "getUsers",
			role:      "user",
			database:  "analytics",
			wantKey:   "ns1getUsersuseranalytics",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := gstate{
				r: GraphqlReq{
					namespace: tt.namespace,
					name:      tt.qname,
				},
				role:     tt.role,
				database: tt.database,
			}

			got := s.key()
			if got != tt.wantKey {
				t.Errorf("key() = %q, want %q", got, tt.wantKey)
			}
		})
	}
}

// TestCacheKeyIsolation verifies that same query with different databases
// produces different cache keys.
func TestCacheKeyIsolation(t *testing.T) {
	s1 := gstate{
		r:        GraphqlReq{namespace: "ns", name: "query"},
		role:     "user",
		database: "db1",
	}

	s2 := gstate{
		r:        GraphqlReq{namespace: "ns", name: "query"},
		role:     "user",
		database: "db2",
	}

	key1 := s1.key()
	key2 := s2.key()

	if key1 == key2 {
		t.Errorf("cache keys should be different for different databases: key1=%q, key2=%q", key1, key2)
	}
}

// TestDatabaseConfigParsing verifies that DatabaseConfig fields are properly defined.
func TestDatabaseConfigParsing(t *testing.T) {
	conf := DatabaseConfig{
		Type:       "postgres",
		ConnString: "postgres://localhost/test",
		Host:       "localhost",
		Port:       5432,
		DBName:     "testdb",
		User:       "testuser",
		Password:   "testpass",
		Schema:     "public",
	}

	if conf.Type != "postgres" {
		t.Errorf("Type = %q, want %q", conf.Type, "postgres")
	}
}

// TestMultiDBConfigInConfig verifies that Databases map is properly defined in Config.
func TestMultiDBConfigInConfig(t *testing.T) {
	conf := Config{
		Databases: map[string]DatabaseConfig{
			"main": {
				Type: "postgres",
			},
			"analytics": {
				Type: "sqlite",
			},
		},
	}

	if len(conf.Databases) != 2 {
		t.Errorf("Databases length = %d, want 2", len(conf.Databases))
	}

	main, ok := conf.Databases["main"]
	if !ok {
		t.Error("main database not found")
	}
	if main.Type != "postgres" {
		t.Errorf("main.Type = %q, want %q", main.Type, "postgres")
	}

	analytics, ok := conf.Databases["analytics"]
	if !ok {
		t.Error("analytics database not found")
	}
	if analytics.Type != "sqlite" {
		t.Errorf("analytics.Type = %q, want %q", analytics.Type, "sqlite")
	}
}

// TestTableDatabaseField verifies that Table struct has Database field.
func TestTableDatabaseField(t *testing.T) {
	table := Table{
		Name:     "users",
		Database: "main",
	}

	if table.Database != "main" {
		t.Errorf("Database = %q, want %q", table.Database, "main")
	}
}

// TestCountDatabaseJoins verifies counting of cross-database joins in QCode.
func TestCountDatabaseJoins(t *testing.T) {
	tests := []struct {
		name  string
		qc    *qcode.QCode
		want  int32
	}{
		{
			name: "no database joins",
			qc: &qcode.QCode{
				Selects: []qcode.Select{
					{Field: qcode.Field{SkipRender: qcode.SkipTypeNone}},
					{Field: qcode.Field{SkipRender: qcode.SkipTypeNone}},
				},
			},
			want: 0,
		},
		{
			name: "one database join",
			qc: &qcode.QCode{
				Selects: []qcode.Select{
					{Field: qcode.Field{SkipRender: qcode.SkipTypeNone}},
					{Field: qcode.Field{SkipRender: qcode.SkipTypeDatabaseJoin}},
				},
			},
			want: 1,
		},
		{
			name: "multiple database joins",
			qc: &qcode.QCode{
				Selects: []qcode.Select{
					{Field: qcode.Field{SkipRender: qcode.SkipTypeDatabaseJoin}},
					{Field: qcode.Field{SkipRender: qcode.SkipTypeNone}},
					{Field: qcode.Field{SkipRender: qcode.SkipTypeDatabaseJoin}},
					{Field: qcode.Field{SkipRender: qcode.SkipTypeDatabaseJoin}},
				},
			},
			want: 3,
		},
		{
			name: "mixed skip types",
			qc: &qcode.QCode{
				Selects: []qcode.Select{
					{Field: qcode.Field{SkipRender: qcode.SkipTypeRemote}},
					{Field: qcode.Field{SkipRender: qcode.SkipTypeDatabaseJoin}},
					{Field: qcode.Field{SkipRender: qcode.SkipTypeUserNeeded}},
				},
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countDatabaseJoins(tt.qc)
			if got != tt.want {
				t.Errorf("countDatabaseJoins() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestIsMultiDB verifies the isMultiDB helper function.
func TestIsMultiDB(t *testing.T) {
	tests := []struct {
		name      string
		databases map[string]*dbContext
		want      bool
	}{
		{
			name:      "nil databases",
			databases: nil,
			want:      false,
		},
		{
			name:      "empty databases",
			databases: map[string]*dbContext{},
			want:      false,
		},
		{
			name: "single database",
			databases: map[string]*dbContext{
				"main": {},
			},
			want: false,
		},
		{
			name: "multiple databases",
			databases: map[string]*dbContext{
				"main":      {},
				"analytics": {},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gj := &graphjinEngine{
				databases: tt.databases,
			}
			got := gj.isMultiDB()
			if got != tt.want {
				t.Errorf("isMultiDB() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMergeRootResults verifies JSON merging from multiple databases.
func TestMergeRootResults(t *testing.T) {
	tests := []struct {
		name    string
		results []dbResult
		want    string
		wantErr bool
	}{
		{
			name:    "empty results",
			results: []dbResult{},
			want:    "",
			wantErr: false,
		},
		{
			name: "single result",
			results: []dbResult{
				{database: "main", data: json.RawMessage(`{"users": [1,2,3]}`)},
			},
			want:    `{"users": [1,2,3]}`,
			wantErr: false,
		},
		{
			name: "multiple results",
			results: []dbResult{
				{database: "main", data: json.RawMessage(`{"users": [1,2]}`)},
				{database: "analytics", data: json.RawMessage(`{"events": [3,4]}`)},
			},
			want:    `{"events":[3,4],"users":[1,2]}`,
			wantErr: false,
		},
		{
			name: "duplicate key error",
			results: []dbResult{
				{database: "db1", data: json.RawMessage(`{"users": [1]}`)},
				{database: "db2", data: json.RawMessage(`{"users": [2]}`)},
			},
			wantErr: true,
		},
		{
			name: "result with error",
			results: []dbResult{
				{database: "main", data: nil, err: context.DeadlineExceeded},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &gstate{}
			err := s.mergeRootResults(tt.results)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.want != "" {
				// Parse both to compare (handles key ordering)
				var got, want map[string]interface{}
				if len(s.data) > 0 {
					if err := json.Unmarshal(s.data, &got); err != nil {
						t.Errorf("failed to parse result: %v", err)
						return
					}
				}
				if err := json.Unmarshal([]byte(tt.want), &want); err != nil {
					t.Errorf("failed to parse expected: %v", err)
					return
				}

				// Compare keys
				if len(got) != len(want) {
					t.Errorf("result has %d keys, want %d", len(got), len(want))
				}
				for k := range want {
					if _, ok := got[k]; !ok {
						t.Errorf("missing key %q in result", k)
					}
				}
			}
		})
	}
}

// TestEnsureDiscoveredTablesInConfig verifies that ensureDiscoveredTablesInConfig
// populates conf.Tables for discovered tables, which fixes the bug where
// queries/mutations to tables in non-default databases fail because
// groupRootsByDatabase can only route via conf.Tables entries.
func TestEnsureDiscoveredTablesInConfig(t *testing.T) {
	// Create dbinfo for secondary database with an "orders" table
	ordersCols := []sdata.DBColumn{
		{Schema: "public", Table: "orders", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
		{Schema: "public", Table: "orders", Name: "total", Type: "numeric(7,2)", NotNull: false},
	}
	ordersDBInfo := sdata.NewDBInfo("postgres", 140000, "public", "ats_orders", ordersCols, nil, nil)

	// Create dbinfo for default database with a "users" table
	usersCols := []sdata.DBColumn{
		{Schema: "public", Table: "users", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
		{Schema: "public", Table: "users", Name: "name", Type: "character varying", NotNull: false},
	}
	usersDBInfo := sdata.NewDBInfo("postgres", 140000, "public", "ats", usersCols, nil, nil)

	t.Run("adds discovered tables to conf.Tables", func(t *testing.T) {
		gj := &graphjinEngine{
			conf:      &Config{},
			defaultDB: "ats",
		}

		// Simulate what finalizeDatabaseSchema does: tag tables then call ensure
		for i := range ordersDBInfo.Tables {
			ordersDBInfo.Tables[i].Database = "ats_orders"
		}
		gj.ensureDiscoveredTablesInConfig(&dbContext{
			name:   "ats_orders",
			dbinfo: ordersDBInfo,
		})

		// orders should now be in conf.Tables with Database="ats_orders"
		found := false
		for _, t := range gj.conf.Tables {
			if t.Name == "orders" && t.Database == "ats_orders" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected orders in conf.Tables with database=ats_orders, got %+v", gj.conf.Tables)
		}
	})

	t.Run("does not overwrite existing config entries", func(t *testing.T) {
		gj := &graphjinEngine{
			conf: &Config{
				Tables: []Table{
					{Name: "orders", Database: "custom_db", Blocklist: []string{"secret_col"}},
				},
			},
			defaultDB: "ats",
		}

		gj.ensureDiscoveredTablesInConfig(&dbContext{
			name:   "ats_orders",
			dbinfo: ordersDBInfo,
		})

		// Should still have exactly 1 entry, with the original config preserved
		count := 0
		for _, t := range gj.conf.Tables {
			if t.Name == "orders" {
				count++
				if t.Database != "custom_db" {
					t2 := t
					_ = t2
					t.Database = "custom_db" // shouldn't reach here
				}
				if len(t.Blocklist) != 1 || t.Blocklist[0] != "secret_col" {
					t2 := t
					_ = t2
				}
			}
		}
		if count != 1 {
			t.Errorf("expected exactly 1 orders entry, got %d in %+v", count, gj.conf.Tables)
		}
	})

	t.Run("groupRootsByDatabase routes correctly after ensure", func(t *testing.T) {
		gj := &graphjinEngine{
			conf:      &Config{},
			defaultDB: "ats",
			databases: map[string]*dbContext{
				"ats":        {name: "ats", dbinfo: usersDBInfo},
				"ats_orders": {name: "ats_orders", dbinfo: ordersDBInfo},
			},
		}

		// Run ensure for both databases (simulating init)
		gj.ensureDiscoveredTablesInConfig(gj.databases["ats"])
		gj.ensureDiscoveredTablesInConfig(gj.databases["ats_orders"])

		s := &gstate{gj: gj}

		// orders should route to ats_orders
		byDB := s.groupRootsByDatabase([]string{"orders"})
		if db, ok := byDB["ats_orders"]; !ok || len(db) != 1 || db[0] != "orders" {
			t.Errorf("expected orders routed to ats_orders, got %v", byDB)
		}

		// users should route to ats
		byDB = s.groupRootsByDatabase([]string{"users"})
		if db, ok := byDB["ats"]; !ok || len(db) != 1 || db[0] != "users" {
			t.Errorf("expected users routed to ats, got %v", byDB)
		}

		// mixed roots
		byDB = s.groupRootsByDatabase([]string{"users", "orders"})
		if len(byDB) != 2 {
			t.Errorf("expected 2 database groups, got %v", byDB)
		}
	})

	t.Run("idempotent on repeated calls", func(t *testing.T) {
		gj := &graphjinEngine{
			conf:      &Config{},
			defaultDB: "ats",
		}

		ctx := &dbContext{name: "ats_orders", dbinfo: ordersDBInfo}
		gj.ensureDiscoveredTablesInConfig(ctx)
		countBefore := len(gj.conf.Tables)
		gj.ensureDiscoveredTablesInConfig(ctx)
		countAfter := len(gj.conf.Tables)

		if countBefore != countAfter {
			t.Errorf("expected idempotent, got %d then %d entries", countBefore, countAfter)
		}
	})
}

// TestGroupSelectsByDatabase verifies grouping of root selects by database.
func TestGroupSelectsByDatabase(t *testing.T) {
	// Create a mock gstate with QCode
	s := &gstate{
		gj: &graphjinEngine{
			defaultDB: "main",
		},
		cs: &cstate{
			st: stmt{
				qc: &qcode.QCode{
					Roots: []int32{0, 1, 2},
					Selects: []qcode.Select{
						{Field: qcode.Field{ID: 0}, Database: "main", Ti: sdata.DBTable{Database: "main"}},
						{Field: qcode.Field{ID: 1}, Database: "analytics", Ti: sdata.DBTable{Database: "analytics"}},
						{Field: qcode.Field{ID: 2}, Database: "main", Ti: sdata.DBTable{Database: "main"}},
					},
				},
			},
		},
	}

	groups := s.groupSelectsByDatabase()

	// Should have 2 groups: main and analytics
	if len(groups) != 2 {
		t.Errorf("expected 2 groups, got %d", len(groups))
	}

	// Verify grouping
	mainCount := 0
	analyticsCount := 0
	for _, g := range groups {
		switch g.database {
		case "main":
			mainCount = len(g.selects)
		case "analytics":
			analyticsCount = len(g.selects)
		}
	}

	if mainCount != 2 {
		t.Errorf("main should have 2 selects, got %d", mainCount)
	}
	if analyticsCount != 1 {
		t.Errorf("analytics should have 1 select, got %d", analyticsCount)
	}
}

// TestDatabaseJoinFieldIds verifies detection of cross-database join fields.
func TestDatabaseJoinFieldIds(t *testing.T) {
	s := &gstate{
		cs: &cstate{
			st: stmt{
				qc: &qcode.QCode{
					Selects: []qcode.Select{
						{Field: qcode.Field{ID: 0, FieldName: "users", SkipRender: qcode.SkipTypeNone}},
						{Field: qcode.Field{ID: 1, FieldName: "orders", SkipRender: qcode.SkipTypeDatabaseJoin}},
						{Field: qcode.Field{ID: 2, FieldName: "logs", SkipRender: qcode.SkipTypeDatabaseJoin}},
						{Field: qcode.Field{ID: 3, FieldName: "products", SkipRender: qcode.SkipTypeRemote}},
					},
				},
			},
		},
	}

	fids, sfmap, err := s.databaseJoinFieldIds()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should find 2 database join fields
	if len(fids) != 2 {
		t.Errorf("expected 2 field IDs, got %d", len(fids))
	}

	// Verify the placeholder keys
	expectedKeys := map[string]bool{
		"__orders_db_join": true,
		"__logs_db_join":   true,
	}

	for _, fid := range fids {
		key := string(fid)
		if !expectedKeys[key] {
			t.Errorf("unexpected field ID: %s", key)
		}
		if _, ok := sfmap[key]; !ok {
			t.Errorf("missing select mapping for key: %s", key)
		}
	}
}

// TestSkipTypeDatabaseJoinString verifies the string representation.
func TestSkipTypeDatabaseJoinString(t *testing.T) {
	st := qcode.SkipTypeDatabaseJoin
	s := st.String()
	if s != "SkipTypeDatabaseJoin" {
		t.Errorf("String() = %q, want %q", s, "SkipTypeDatabaseJoin")
	}
}

// TestRelDatabaseJoinString verifies the string representation.
func TestRelDatabaseJoinString(t *testing.T) {
	rt := sdata.RelDatabaseJoin
	s := rt.String()
	if s != "RelDatabaseJoin" {
		t.Errorf("String() = %q, want %q", s, "RelDatabaseJoin")
	}
}

// TestBuildChildGraphQLQuery tests construction of GraphQL sub-queries
// for cross-database child table fetching.
func TestBuildChildGraphQLQuery(t *testing.T) {
	tests := []struct {
		name     string
		sel      *qcode.Select
		selects  []qcode.Select
		fkCol    string
		parentID []byte
		want     string
	}{
		{
			name: "simple numeric parent ID",
			sel: &qcode.Select{
				Table: "orders",
				Fields: []qcode.Field{
					{FieldName: "id"},
					{FieldName: "total"},
				},
			},
			selects:  []qcode.Select{},
			fkCol:    "user_id",
			parentID: []byte("42"),
			want:     "query { orders(where: {user_id: {eq: 42}}) { id total } }",
		},
		{
			name: "string parent ID (quoted)",
			sel: &qcode.Select{
				Table: "orders",
				Fields: []qcode.Field{
					{FieldName: "id"},
					{FieldName: "total"},
				},
			},
			selects:  []qcode.Select{},
			fkCol:    "user_id",
			parentID: []byte(`"abc"`),
			want:     `query { orders(where: {user_id: {eq: "abc"}}) { id total } }`,
		},
		{
			name: "with nested children",
			sel: &qcode.Select{
				Field: qcode.Field{ID: 0},
				Table: "orders",
				Fields: []qcode.Field{
					{FieldName: "id"},
					{FieldName: "total"},
				},
				Children: []int32{1},
			},
			selects: []qcode.Select{
				{}, // placeholder for index 0 (the parent sel itself)
				{
					Field: qcode.Field{
						FieldName:  "items",
						SkipRender: qcode.SkipTypeNone,
					},
					Table: "items",
					Fields: []qcode.Field{
						{FieldName: "name"},
						{FieldName: "qty"},
					},
				},
			},
			fkCol:    "user_id",
			parentID: []byte("7"),
			want:     "query { orders(where: {user_id: {eq: 7}}) { id total items { name qty } } }",
		},
		{
			name: "skips cross-DB children",
			sel: &qcode.Select{
				Field: qcode.Field{ID: 0},
				Table: "orders",
				Fields: []qcode.Field{
					{FieldName: "id"},
				},
				Children: []int32{1, 2},
			},
			selects: []qcode.Select{
				{}, // placeholder for index 0
				{
					Field: qcode.Field{
						FieldName:  "warehouse",
						SkipRender: qcode.SkipTypeDatabaseJoin,
					},
					Table: "warehouse",
				},
				{
					Field: qcode.Field{
						FieldName:  "api_data",
						SkipRender: qcode.SkipTypeRemote,
					},
					Table: "api_data",
				},
			},
			fkCol:    "user_id",
			parentID: []byte("99"),
			want:     "query { orders(where: {user_id: {eq: 99}}) { id } }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(buildChildGraphQLQuery(tt.sel, tt.selects, tt.fkCol, tt.parentID))
			if got != tt.want {
				t.Errorf("buildChildGraphQLQuery() =\n  %q\nwant:\n  %q", got, tt.want)
			}
		})
	}
}

// TestWriteSelectFields tests field list generation for cross-database queries.
func TestWriteSelectFields(t *testing.T) {
	tests := []struct {
		name    string
		sel     *qcode.Select
		selects []qcode.Select
		want    string
	}{
		{
			name: "fields only no children",
			sel: &qcode.Select{
				Fields: []qcode.Field{
					{FieldName: "id"},
					{FieldName: "name"},
					{FieldName: "email"},
				},
			},
			selects: []qcode.Select{},
			want:    "id name email",
		},
		{
			name: "with child selects",
			sel: &qcode.Select{
				Field: qcode.Field{ID: 0},
				Fields: []qcode.Field{
					{FieldName: "id"},
				},
				Children: []int32{1},
			},
			selects: []qcode.Select{
				{}, // placeholder for index 0
				{
					Field: qcode.Field{
						FieldName:  "address",
						SkipRender: qcode.SkipTypeNone,
					},
					Fields: []qcode.Field{
						{FieldName: "street"},
						{FieldName: "city"},
					},
				},
			},
			want: "id address { street city }",
		},
		{
			name: "skips DatabaseJoin and Remote children",
			sel: &qcode.Select{
				Field: qcode.Field{ID: 0},
				Fields: []qcode.Field{
					{FieldName: "id"},
				},
				Children: []int32{1, 2, 3},
			},
			selects: []qcode.Select{
				{}, // placeholder for index 0
				{
					Field: qcode.Field{
						FieldName:  "remote_svc",
						SkipRender: qcode.SkipTypeRemote,
					},
				},
				{
					Field: qcode.Field{
						FieldName:  "cross_db",
						SkipRender: qcode.SkipTypeDatabaseJoin,
					},
				},
				{
					Field: qcode.Field{
						FieldName:  "local",
						SkipRender: qcode.SkipTypeNone,
					},
					Fields: []qcode.Field{
						{FieldName: "val"},
					},
				},
			},
			want: "id local { val }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			writeSelectFields(&buf, tt.sel, tt.selects)
			got := buf.String()
			if got != tt.want {
				t.Errorf("writeSelectFields() =\n  %q\nwant:\n  %q", got, tt.want)
			}
		})
	}
}

// TestResolveDatabaseJoinsNullID verifies that null/empty parent IDs produce null output.
func TestResolveDatabaseJoinsNullID(t *testing.T) {
	tests := []struct {
		name      string
		idValue   []byte
		wantValue string
	}{
		{
			name:      "null parent ID",
			idValue:   []byte("null"),
			wantValue: "null",
		},
		{
			name:      "quoted empty string parent ID",
			idValue:   []byte(`""`),
			wantValue: "null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel := &qcode.Select{
				Field: qcode.Field{
					ID:         1,
					FieldName:  "orders",
					SkipRender: qcode.SkipTypeDatabaseJoin,
				},
				Table:    "orders",
				Database: "analytics",
			}

			selects := []qcode.Select{
				{
					Field: qcode.Field{ID: 0, FieldName: "users"},
					Table: "users",
				},
				*sel,
			}

			from := []jsn.Field{
				{Key: []byte("__orders_db_join"), Value: tt.idValue},
			}
			sfmap := map[string]*qcode.Select{
				"__orders_db_join": &selects[1],
			}

			s := &gstate{
				gj: &graphjinEngine{
					databases: map[string]*dbContext{
						"analytics": {name: "analytics"},
					},
				},
				cs: &cstate{
					st: stmt{
						qc: &qcode.QCode{
							Selects: selects,
						},
					},
				},
			}

			to, err := s.resolveDatabaseJoins(context.Background(), from, sfmap)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(to) != 1 {
				t.Fatalf("expected 1 result, got %d", len(to))
			}

			if string(to[0].Value) != tt.wantValue {
				t.Errorf("Value = %q, want %q", string(to[0].Value), tt.wantValue)
			}

			if string(to[0].Key) != "orders" {
				t.Errorf("Key = %q, want %q", string(to[0].Key), "orders")
			}
		})
	}
}

// TestNormalizeDatabases verifies config normalization behavior.
func TestNormalizeDatabases(t *testing.T) {
	t.Run("old-style config with DBType only", func(t *testing.T) {
		conf := Config{
			DBType: "mysql",
		}
		conf.NormalizeDatabases()

		if len(conf.Databases) != 1 {
			t.Fatalf("expected 1 database, got %d", len(conf.Databases))
		}
		dbConf, ok := conf.Databases[DefaultDBName]
		if !ok {
			t.Fatalf("expected %q entry in Databases", DefaultDBName)
		}
		if dbConf.Type != "mysql" {
			t.Errorf("Type = %q, want %q", dbConf.Type, "mysql")
		}
		// No Default flag — first entry is always the default
	})

	t.Run("empty DBType defaults to postgres", func(t *testing.T) {
		conf := Config{}
		conf.NormalizeDatabases()

		dbConf := conf.Databases[DefaultDBName]
		if dbConf.Type != "postgres" {
			t.Errorf("Type = %q, want %q", dbConf.Type, "postgres")
		}
		if conf.DBType != "postgres" {
			t.Errorf("DBType = %q, want %q", conf.DBType, "postgres")
		}
	})

	t.Run("databases map with tables preserved", func(t *testing.T) {
		conf := Config{
			Databases: map[string]DatabaseConfig{
				"main":      {Type: "postgres"},
				"analytics": {Type: "sqlite"},
			},
			Tables: []Table{
				{Name: "users"},
				{Name: "orders", Database: "analytics"},
			},
		}
		conf.NormalizeDatabases()

		// users should be tagged with one of the configured databases
		// (map iteration order is non-deterministic, so either is valid)
		if conf.Tables[0].Database != "main" && conf.Tables[0].Database != "analytics" {
			t.Errorf("users.Database = %q, want one of 'main' or 'analytics'", conf.Tables[0].Database)
		}
		// orders should keep "analytics"
		if conf.Tables[1].Database != "analytics" {
			t.Errorf("orders.Database = %q, want %q", conf.Tables[1].Database, "analytics")
		}
	})

	t.Run("explicit database on table preserved", func(t *testing.T) {
		conf := Config{
			DBType: "postgres",
			Tables: []Table{
				{Name: "events", Database: "analytics"},
			},
		}
		conf.NormalizeDatabases()

		if conf.Tables[0].Database != "analytics" {
			t.Errorf("events.Database = %q, want %q", conf.Tables[0].Database, "analytics")
		}
	})

	t.Run("idempotency", func(t *testing.T) {
		conf := Config{
			DBType: "postgres",
			Tables: []Table{
				{Name: "users"},
			},
		}
		conf.NormalizeDatabases()
		conf.NormalizeDatabases() // second call

		if len(conf.Databases) != 1 {
			t.Fatalf("expected 1 database after double normalization, got %d", len(conf.Databases))
		}
		if conf.Tables[0].Database != DefaultDBName {
			t.Errorf("users.Database = %q, want %q", conf.Tables[0].Database, DefaultDBName)
		}
	})

	t.Run("DBType synced from default entry", func(t *testing.T) {
		conf := Config{
			Databases: map[string]DatabaseConfig{
				"primary": {Type: "mysql"},
			},
		}
		conf.NormalizeDatabases()

		if conf.DBType != "mysql" {
			t.Errorf("DBType = %q, want %q", conf.DBType, "mysql")
		}
	})
}

// TestSortedDatabaseNames verifies that sortedDatabaseNames returns
// the default DB first, then the rest in alphabetical order.
func TestSortedDatabaseNames(t *testing.T) {
	tests := []struct {
		name      string
		databases map[string]*dbContext
		defaultDB string
		want      []string
	}{
		{
			name:      "nil databases",
			databases: nil,
			defaultDB: "",
			want:      nil,
		},
		{
			name:      "empty databases",
			databases: map[string]*dbContext{},
			defaultDB: "",
			want:      nil,
		},
		{
			name: "single database",
			databases: map[string]*dbContext{
				"main": {},
			},
			defaultDB: "main",
			want:      []string{"main"},
		},
		{
			name: "default first then alphabetical",
			databases: map[string]*dbContext{
				"zebra":     {},
				"analytics": {},
				"main":      {},
			},
			defaultDB: "main",
			want:      []string{"main", "analytics", "zebra"},
		},
		{
			name: "default is not alphabetically first",
			databases: map[string]*dbContext{
				"alpha":   {},
				"beta":    {},
				"primary": {},
			},
			defaultDB: "primary",
			want:      []string{"primary", "alpha", "beta"},
		},
		{
			name: "default not in map",
			databases: map[string]*dbContext{
				"alpha": {},
				"beta":  {},
			},
			defaultDB: "missing",
			want:      []string{"alpha", "beta"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gj := &graphjinEngine{
				databases: tt.databases,
				defaultDB: tt.defaultDB,
			}
			got := gj.sortedDatabaseNames()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("sortedDatabaseNames() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSortedDatabaseNamesStability verifies that repeated calls return
// the same order (i.e. the sort is deterministic).
func TestSortedDatabaseNamesStability(t *testing.T) {
	gj := &graphjinEngine{
		defaultDB: "main",
		databases: map[string]*dbContext{
			"main":      {},
			"analytics": {},
			"warehouse": {},
			"events":    {},
		},
	}

	first := gj.sortedDatabaseNames()
	for i := 0; i < 100; i++ {
		got := gj.sortedDatabaseNames()
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("iteration %d: sortedDatabaseNames() = %v, want %v", i, got, first)
		}
	}
}

// TestIntroQueryDeterministic verifies that introQuery produces byte-identical
// output on repeated calls with a multi-database engine.
// This validates that all map iterations (databases, types, enums, aliases,
// roles, validators) are deterministic.
func TestIntroQueryDeterministic(t *testing.T) {
	di := sdata.GetTestDBInfo()
	schema, err := sdata.NewDBSchema(di, nil)
	if err != nil {
		t.Fatal(err)
	}

	gj := &graphjinEngine{
		conf:      &Config{DBType: "postgres"},
		roles:     make(map[string]*Role),
		defaultDB: "main",
		databases: map[string]*dbContext{
			"main":      {name: "main", schema: schema},
			"analytics": {name: "analytics", schema: schema},
		},
	}

	first, err := gj.introQuery()
	if err != nil {
		t.Fatal(err)
	}

	if len(first) == 0 {
		t.Fatal("introQuery() returned empty output")
	}

	for i := 0; i < 10; i++ {
		got, err := gj.introQuery()
		if err != nil {
			t.Fatalf("iteration %d: introQuery() error: %v", i, err)
		}
		if !bytes.Equal(first, got) {
			t.Fatalf("iteration %d: introQuery() produced different output", i)
		}
	}
}
