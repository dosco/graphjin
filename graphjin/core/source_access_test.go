package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
	_ "github.com/mattn/go-sqlite3"
)

func TestApplySourceAccessRulesGeneratesAccountFiltersAndClassifications(t *testing.T) {
	conf := &Config{
		Sources: []SourceConfig{{
			Name: "app",
			Kind: "database",
			Access: SourceAccessConfig{
				Read:                   "account",
				Write:                  "account",
				Delete:                 "blocked",
				NamespaceColumn:        "account_id",
				MissingNamespaceColumn: "block",
				PublicTables:           []string{"countries"},
				AdminTables:            []string{"audit_logs"},
				BlockedTables:          []string{"internal_events"},
			},
		}},
		Identity: IdentityConfig{AdminRoles: []string{"admin"}},
		Roles:    []Role{{Name: "support"}},
	}
	if err := conf.NormalizeSources(); err != nil {
		t.Fatalf("NormalizeSources: %v", err)
	}
	di := sdata.NewDBInfo("postgres", 0, "public", "app", []sdata.DBColumn{
		{Schema: "public", Table: "orders", Name: "id", Type: "bigint", PrimaryKey: true},
		{Schema: "public", Table: "orders", Name: "account_id", Type: "bigint"},
		{Schema: "public", Table: "countries", Name: "id", Type: "bigint", PrimaryKey: true},
		{Schema: "public", Table: "audit_logs", Name: "id", Type: "bigint", PrimaryKey: true},
		{Schema: "public", Table: "audit_logs", Name: "account_id", Type: "bigint"},
		{Schema: "public", Table: "internal_events", Name: "id", Type: "bigint", PrimaryKey: true},
		{Schema: "public", Table: "invoices", Name: "id", Type: "bigint", PrimaryKey: true},
	}, nil, nil)
	for i := range di.Tables {
		di.Tables[i].Database = "app"
	}
	gj := &graphjinEngine{conf: conf}
	if err := gj.applySourceAccessRules(di, "app"); err != nil {
		t.Fatalf("applySourceAccessRules: %v", err)
	}

	userOrders := roleTable(t, conf, "user", "orders")
	if userOrders.Query == nil || len(userOrders.Query.Filters) != 1 || userOrders.Query.Filters[0] != "{ account_id: { eq: $account_id } }" {
		t.Fatalf("user orders query filters not generated: %+v", userOrders.Query)
	}
	if userOrders.Insert == nil || userOrders.Insert.Presets["account_id"] != "$account_id" {
		t.Fatalf("user orders insert preset not generated: %+v", userOrders.Insert)
	}
	if userOrders.Delete == nil || !userOrders.Delete.Block {
		t.Fatalf("delete should be blocked by default: %+v", userOrders.Delete)
	}

	anonCountries := roleTable(t, conf, "anon", "countries")
	if anonCountries.Query == nil || anonCountries.Query.Block {
		t.Fatalf("public table should be queryable by anon: %+v", anonCountries.Query)
	}
	if !anonCountries.Insert.Block || !anonCountries.Update.Block || !anonCountries.Delete.Block {
		t.Fatalf("public table should be read-only: %+v", anonCountries)
	}

	userAudit := roleTable(t, conf, "user", "audit_logs")
	adminAudit := roleTable(t, conf, "admin", "audit_logs")
	if userAudit.Query == nil || !userAudit.Query.Block {
		t.Fatalf("admin table should block user reads: %+v", userAudit.Query)
	}
	if adminAudit.Query == nil || adminAudit.Query.Block {
		t.Fatalf("admin table should allow admin reads: %+v", adminAudit.Query)
	}

	if table, err := di.GetTable("public", "internal_events"); err != nil || !table.Blocked {
		t.Fatalf("blocked table not marked blocked: table=%+v err=%v", table, err)
	}
	userInvoices := roleTable(t, conf, "user", "invoices")
	if userInvoices.Query == nil || !userInvoices.Query.Block {
		t.Fatalf("table missing account_id should block account read: %+v", userInvoices.Query)
	}
}

func TestApplySourceAccessRulesMissingNamespaceAllowIsUnscopedAuthenticated(t *testing.T) {
	conf := &Config{
		Sources: []SourceConfig{{
			Name: "app",
			Kind: "database",
			Access: SourceAccessConfig{
				Read:                   AccessModeAccount,
				NamespaceColumn:        "account_id",
				MissingNamespaceColumn: MissingNamespaceAllow,
			},
		}},
	}
	if err := conf.NormalizeSources(); err != nil {
		t.Fatalf("NormalizeSources: %v", err)
	}
	di := sdata.NewDBInfo("postgres", 0, "public", "app", []sdata.DBColumn{
		{Schema: "public", Table: "invoices", Name: "id", Type: "bigint", PrimaryKey: true},
	}, nil, nil)
	for i := range di.Tables {
		di.Tables[i].Database = "app"
	}
	gj := &graphjinEngine{conf: conf}
	if err := gj.applySourceAccessRules(di, "app"); err != nil {
		t.Fatalf("applySourceAccessRules: %v", err)
	}
	userInvoices := roleTable(t, conf, "user", "invoices")
	if userInvoices.Query == nil || userInvoices.Query.Block || len(userInvoices.Query.Filters) != 0 {
		t.Fatalf("missing namespace allow should become authenticated unscoped read: %+v", userInvoices.Query)
	}
	anonInvoices := roleTable(t, conf, "anon", "invoices")
	if anonInvoices.Query == nil || !anonInvoices.Query.Block {
		t.Fatalf("missing namespace allow should still block anon account reads: %+v", anonInvoices.Query)
	}
}

func roleTable(t *testing.T, conf *Config, role, table string) RoleTable {
	t.Helper()
	for _, r := range conf.Roles {
		if r.Name != role {
			continue
		}
		for _, rt := range r.Tables {
			if rt.Name == table {
				return rt
			}
		}
	}
	t.Fatalf("role table not found: %s.%s in %+v", role, table, conf.Roles)
	return RoleTable{}
}

func TestSourceModeBlockedTableReturnsUnauthorized(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE internal_events (id INTEGER PRIMARY KEY, account_id TEXT)`); err != nil {
		t.Fatal(err)
	}

	conf := &Config{
		DBType:           "sqlite",
		DisableAllowList: true,
		Sources: []SourceConfig{{
			Name:    "main",
			Kind:    "database",
			Type:    "sqlite",
			Default: true,
			Access: SourceAccessConfig{
				Read:          AccessModeAccount,
				BlockedTables: []string{"internal_events"},
			},
		}},
	}
	gj, err := NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	ctx := context.WithValue(context.Background(), UserIDKey, "user_1")
	ctx = context.WithValue(ctx, IdentityVarsKey, map[string]interface{}{"account_id": "acct_1"})
	_, err = gj.GraphQL(ctx, `query { internal_events { id } }`, nil, nil)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "unauthorized") {
		t.Fatalf("expected unauthorized blocked-table error, got %v", err)
	}
}

func TestSourceModeAccountVariableMustComeFromIdentity(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, account_id TEXT);
INSERT INTO users (id, account_id) VALUES (1, 'acct_1'), (2, 'acct_2')`); err != nil {
		t.Fatal(err)
	}

	conf := &Config{
		DBType:           "sqlite",
		DisableAllowList: true,
		Sources: []SourceConfig{{
			Name:    "main",
			Kind:    "database",
			Type:    "sqlite",
			Default: true,
			Access: SourceAccessConfig{
				Read:            AccessModeAccount,
				NamespaceColumn: "account_id",
			},
		}},
	}
	gj, err := NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	ctx := context.WithValue(context.Background(), UserIDKey, "user_1")
	_, err = gj.GraphQL(ctx, `query { users { id account_id } }`, json.RawMessage(`{"account_id":"acct_1"}`), nil)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "identity variable 'account_id' is required") {
		t.Fatalf("expected trusted identity variable error, got %v", err)
	}

	ctx = context.WithValue(ctx, IdentityVarsKey, map[string]interface{}{"account_id": "acct_1"})
	res, err := gj.GraphQL(ctx, `query { users { id account_id } }`, json.RawMessage(`{"account_id":"acct_2"}`), nil)
	if err != nil {
		t.Fatalf("GraphQL with trusted account_id: %v", err)
	}
	var out struct {
		Users []struct {
			ID        int    `json:"id"`
			AccountID string `json:"account_id"`
		} `json:"users"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("decode users: %v\n%s", err, string(res.Data))
	}
	if len(out.Users) != 1 || out.Users[0].ID != 1 || out.Users[0].AccountID != "acct_1" {
		t.Fatalf("expected trusted account filter to override client variable, got %s", string(res.Data))
	}
}
