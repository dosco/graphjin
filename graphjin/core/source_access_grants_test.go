package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func grantsConfig(access SourceAccessConfig) *Config {
	return &Config{
		Sources:  []SourceConfig{{Name: "app", Kind: "database", Access: access}},
		Identity: IdentityConfig{AdminRoles: []string{"admin"}},
		Roles:    []Role{{Name: "finance"}, {Name: "sales"}, {Name: "admin"}},
	}
}

func financeGrant(tables ...SourceAccessGrantTable) SourceAccessGrant {
	return SourceAccessGrant{Role: "finance", Tables: tables}
}

func TestValidateSourceAccessGrants(t *testing.T) {
	orders := SourceAccessGrantTable{Name: "orders", Columns: []string{"id"}}
	tests := []struct {
		name    string
		kind    string
		access  SourceAccessConfig
		wantErr string
	}{
		{name: "valid", access: SourceAccessConfig{Grants: []SourceAccessGrant{
			financeGrant(orders),
			{Role: "sales", Tables: []SourceAccessGrantTable{orders}},
			{Role: "user", Tables: []SourceAccessGrantTable{orders}},
			{Role: "anon", Tables: []SourceAccessGrantTable{orders}},
		}}},
		{name: "api source", kind: "api", access: SourceAccessConfig{Grants: []SourceAccessGrant{financeGrant(orders)}},
			wantErr: "database sources only"},
		{name: "missing role", access: SourceAccessConfig{Grants: []SourceAccessGrant{{Tables: []SourceAccessGrantTable{orders}}}},
			wantErr: "role is required"},
		{name: "reserved role", access: SourceAccessConfig{Grants: []SourceAccessGrant{{Role: "__system", Tables: []SourceAccessGrantTable{orders}}}},
			wantErr: "is reserved"},
		{name: "admin role", access: SourceAccessConfig{Grants: []SourceAccessGrant{{Role: "Admin", Tables: []SourceAccessGrantTable{orders}}}},
			wantErr: "admin role"},
		{name: "undefined role", access: SourceAccessConfig{Grants: []SourceAccessGrant{{Role: "ops", Tables: []SourceAccessGrantTable{orders}}}},
			wantErr: `role "ops" is not defined`},
		{name: "no tables", access: SourceAccessConfig{Grants: []SourceAccessGrant{financeGrant()}},
			wantErr: "tables is required"},
		{name: "no table name", access: SourceAccessConfig{Grants: []SourceAccessGrant{financeGrant(SourceAccessGrantTable{Columns: []string{"id"}})}},
			wantErr: "name is required"},
		{name: "no columns", access: SourceAccessConfig{Grants: []SourceAccessGrant{financeGrant(SourceAccessGrantTable{Name: "orders"})}},
			wantErr: "columns is required"},
		{name: "blank column", access: SourceAccessConfig{Grants: []SourceAccessGrant{financeGrant(SourceAccessGrantTable{Name: "orders", Columns: []string{"id", " "}})}},
			wantErr: "column names must not be empty"},
		{name: "blocked table", access: SourceAccessConfig{BlockedTables: []string{"Orders"}, Grants: []SourceAccessGrant{financeGrant(orders)}},
			wantErr: "is in blocked_tables"},
		{name: "duplicate table", access: SourceAccessConfig{Grants: []SourceAccessGrant{
			financeGrant(orders),
			{Role: "FINANCE", Tables: []SourceAccessGrantTable{{Name: "ORDERS", Columns: []string{"id"}}}},
		}}, wantErr: "more than one grant"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := grantsConfig(tt.access)
			if tt.kind != "" {
				conf.Sources[0].Kind = tt.kind
			}
			err := conf.NormalizeSources()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("NormalizeSources: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func grantsDBInfo() *sdata.DBInfo {
	di := sdata.NewDBInfo("postgres", 0, "public", "app", []sdata.DBColumn{
		{Schema: "public", Table: "orders", Name: "id", Type: "bigint", PrimaryKey: true},
		{Schema: "public", Table: "orders", Name: "account_id", Type: "bigint"},
		{Schema: "public", Table: "orders", Name: "user_id", Type: "bigint"},
		{Schema: "public", Table: "orders", Name: "region", Type: "text"},
		{Schema: "public", Table: "orders", Name: "amount", Type: "numeric"},
		{Schema: "public", Table: "notes", Name: "id", Type: "bigint", PrimaryKey: true},
		{Schema: "public", Table: "events", Name: "id", Type: "bigint", PrimaryKey: true},
	}, nil, nil)
	for i := range di.Tables {
		di.Tables[i].Database = "app"
	}
	return di
}

func applyGrants(t *testing.T, access SourceAccessConfig) (*Config, error) {
	t.Helper()
	conf := grantsConfig(access)
	if err := conf.NormalizeSources(); err != nil {
		t.Fatalf("NormalizeSources: %v", err)
	}
	gj := &graphjinEngine{conf: conf}
	return conf, gj.applySourceAccessRules(grantsDBInfo(), "app")
}

func TestApplySourceGrantsPerReadMode(t *testing.T) {
	grant := financeGrant(SourceAccessGrantTable{
		Name:    "public.orders",
		Columns: []string{"id", "amount"},
		Filter:  `{ region: { eq: "emea" } }`,
	})
	tests := []struct {
		mode        string
		wantFilters []string
	}{
		{mode: AccessModeAdmin, wantFilters: []string{`{ region: { eq: "emea" } }`}},
		{mode: AccessModePublic, wantFilters: []string{`{ region: { eq: "emea" } }`}},
		{mode: AccessModeAuthenticated, wantFilters: []string{`{ region: { eq: "emea" } }`}},
		{mode: AccessModeAccount, wantFilters: []string{"{ account_id: { eq: $account_id } }", `{ region: { eq: "emea" } }`}},
		{mode: AccessModeOwner, wantFilters: []string{"{ user_id: { eq: $user_id } }", `{ region: { eq: "emea" } }`}},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			conf, err := applyGrants(t, SourceAccessConfig{Read: tt.mode, Write: AccessModeAccount, Grants: []SourceAccessGrant{grant}})
			if err != nil {
				t.Fatalf("applySourceAccessRules: %v", err)
			}
			fin := roleTable(t, conf, "finance", "orders")
			if fin.Query == nil || fin.Query.Block {
				t.Fatalf("grant should open the read: %+v", fin.Query)
			}
			if !reflect.DeepEqual(fin.Query.Columns, []string{"id", "amount"}) {
				t.Fatalf("columns = %v", fin.Query.Columns)
			}
			if !reflect.DeepEqual(fin.Query.Filters, tt.wantFilters) {
				t.Fatalf("filters = %q, want %q", fin.Query.Filters, tt.wantFilters)
			}
			if fin.Insert == nil || fin.Insert.Block || fin.Insert.Presets["account_id"] != "$account_id" {
				t.Fatalf("grant must not change the write rule: %+v", fin.Insert)
			}

			sales := roleTable(t, conf, "sales", "orders")
			if len(sales.Query.Columns) != 0 || reflect.DeepEqual(sales.Query.Filters, tt.wantFilters) {
				t.Fatalf("role without a grant must keep the source mode: %+v", sales.Query)
			}
			if tt.mode == AccessModeAdmin && !sales.Query.Block {
				t.Fatalf("admin mode must still block a role without a grant: %+v", sales.Query)
			}

			admin := roleTable(t, conf, "admin", "orders")
			if admin.Query.Block || len(admin.Query.Columns) != 0 || len(admin.Query.Filters) != 0 {
				t.Fatalf("admin role must keep full access: %+v", admin.Query)
			}
			notes := roleTable(t, conf, "finance", "notes")
			if len(notes.Query.Columns) != 0 {
				t.Fatalf("grant must apply to its table only: %+v", notes.Query)
			}
		})
	}
}

func TestApplySourceGrantsIsIdempotent(t *testing.T) {
	conf := grantsConfig(SourceAccessConfig{Read: AccessModeAdmin, Grants: []SourceAccessGrant{
		financeGrant(SourceAccessGrantTable{Name: "orders", Columns: []string{"id"}}),
	}})
	if err := conf.NormalizeSources(); err != nil {
		t.Fatal(err)
	}
	gj := &graphjinEngine{conf: conf}
	for i := 0; i < 2; i++ {
		if err := gj.applySourceAccessRules(grantsDBInfo(), "app"); err != nil {
			t.Fatalf("apply %d: %v", i, err)
		}
	}
	for _, r := range conf.Roles {
		if r.Name != "finance" {
			continue
		}
		n := 0
		for _, rt := range r.Tables {
			if rt.Name == "orders" {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("finance has %d orders tables, want 1", n)
		}
	}
}

func TestApplySourceGrantsErrors(t *testing.T) {
	tests := []struct {
		name    string
		access  SourceAccessConfig
		wantErr string
	}{
		{name: "unknown table", access: SourceAccessConfig{Read: AccessModeAdmin, Grants: []SourceAccessGrant{
			financeGrant(SourceAccessGrantTable{Name: "invoices", Columns: []string{"id"}}),
		}}, wantErr: `table "invoices" not found`},
		{name: "unknown column", access: SourceAccessConfig{Read: AccessModeAdmin, Grants: []SourceAccessGrant{
			financeGrant(SourceAccessGrantTable{Name: "orders", Columns: []string{"id", "margin"}}),
		}}, wantErr: `has no column "margin"`},
		{name: "blocked table by schema name", access: SourceAccessConfig{Read: AccessModeAdmin, BlockedTables: []string{"events"}, Grants: []SourceAccessGrant{
			financeGrant(SourceAccessGrantTable{Name: "public.events", Columns: []string{"id"}}),
		}}, wantErr: "the table is in blocked_tables"},
		{name: "missing namespace column", access: SourceAccessConfig{Read: AccessModeAccount, MissingNamespaceColumn: MissingNamespaceBlock, Grants: []SourceAccessGrant{
			financeGrant(SourceAccessGrantTable{Name: "notes", Columns: []string{"id"}}),
		}}, wantErr: "read access is blocked"},
		{name: "blocked read mode", access: SourceAccessConfig{Read: AccessModeBlocked, Grants: []SourceAccessGrant{
			financeGrant(SourceAccessGrantTable{Name: "orders", Columns: []string{"id"}}),
		}}, wantErr: "read access is blocked"},
		{name: "two names for one table", access: SourceAccessConfig{Read: AccessModeAdmin, Grants: []SourceAccessGrant{
			financeGrant(
				SourceAccessGrantTable{Name: "orders", Columns: []string{"id"}},
				SourceAccessGrantTable{Name: "public.orders", Columns: []string{"id", "amount"}},
			),
		}}, wantErr: "more than one grant"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := applyGrants(t, tt.access)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestSourceAccessGrantsClone(t *testing.T) {
	access := SourceAccessConfig{Grants: []SourceAccessGrant{
		financeGrant(SourceAccessGrantTable{Name: "orders", Columns: []string{"id"}}),
	}}
	out := access.clone()
	out.Grants[0].Role = "sales"
	out.Grants[0].Tables[0].Name = "notes"
	out.Grants[0].Tables[0].Columns[0] = "amount"
	if access.Grants[0].Role != "finance" || access.Grants[0].Tables[0].Name != "orders" || access.Grants[0].Tables[0].Columns[0] != "id" {
		t.Fatalf("clone shares grant data: %+v", access.Grants)
	}
}

func TestSourceModeGrantsQuery(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE orders (id INTEGER PRIMARY KEY, account_id TEXT, region TEXT, amount INTEGER);
INSERT INTO orders (id, account_id, region, amount) VALUES
	(1, 'acct_1', 'emea', 10),
	(2, 'acct_1', 'apac', 20),
	(3, 'acct_2', 'emea', 30)`); err != nil {
		t.Fatal(err)
	}

	newEngine := func(t *testing.T, mode string) *GraphJin {
		t.Helper()
		conf := &Config{
			DBType:           "sqlite",
			DisableAllowList: true,
			Identity:         IdentityConfig{RoleMode: mode},
			Roles:            []Role{{Name: "finance"}, {Name: "sales"}},
			Sources: []SourceConfig{{
				Name:    "main",
				Kind:    "database",
				Type:    "sqlite",
				Default: true,
				Access: SourceAccessConfig{
					Read: AccessModeAccount,
					Grants: []SourceAccessGrant{
						{Role: "finance", Tables: []SourceAccessGrantTable{{
							Name: "orders", Columns: []string{"id", "region", "amount"}, Filter: `{ region: { eq: "emea" } }`,
						}}},
						{Role: "sales", Tables: []SourceAccessGrantTable{{
							Name: "orders", Columns: []string{"id", "region"},
						}}},
					},
				},
			}},
		}
		gj, err := NewGraphJin(conf, db)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { gj.Close() })
		return gj
	}
	caller := func(roles ...string) context.Context {
		ctx := context.WithValue(context.Background(), UserIDKey, "user_1")
		ctx = context.WithValue(ctx, IdentityVarsKey, map[string]interface{}{"account_id": "acct_1"})
		if len(roles) != 0 {
			ctx = context.WithValue(ctx, IdentityRolesKey, roles)
		}
		return ctx
	}
	ids := func(t *testing.T, gj *GraphJin, ctx context.Context, query string) []int {
		t.Helper()
		res, err := gj.GraphQL(ctx, query, nil, nil)
		if err != nil {
			t.Fatalf("GraphQL: %v", err)
		}
		var out struct {
			Orders []struct {
				ID int `json:"id"`
			} `json:"orders"`
		}
		if err := json.Unmarshal(res.Data, &out); err != nil {
			t.Fatalf("decode: %v\n%s", err, res.Data)
		}
		got := []int{}
		for _, o := range out.Orders {
			got = append(got, o.ID)
		}
		return got
	}

	first := newEngine(t, RoleModeFirst)
	if got := ids(t, first, caller("finance"), `query { orders(order_by: { id: asc }) { id amount } }`); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("finance should read emea rows of its account only, got %v", got)
	}
	if _, err := first.GraphQL(caller("sales"), `query { orders { id amount } }`, nil, nil); err == nil || !strings.Contains(err.Error(), "amount") {
		t.Fatalf("sales should not read amount, got %v", err)
	}
	if got := ids(t, first, caller(), `query { orders(order_by: { id: asc }) { id amount } }`); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("a caller without a grant role should keep the account mode, got %v", got)
	}

	union := newEngine(t, RoleModeUnion)
	if got := ids(t, union, caller("sales", "finance"), `query { orders(order_by: { id: asc }) { id region } }`); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("finance+sales should read every row of its account, got %v", got)
	}
	if _, err := union.GraphQL(caller("sales", "finance"), `query { orders { id amount } }`, nil, nil); err == nil || !strings.Contains(err.Error(), "amount") {
		t.Fatalf("finance+sales must not read amount on apac rows, got %v", err)
	}
}
