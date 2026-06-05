package core

import (
	"strings"
	"testing"
)

func TestValidateDBType(t *testing.T) {
	tests := []struct {
		name    string
		dbType  string
		wantErr bool
	}{
		{"empty string defaults to postgres", "", false},
		{"postgres is valid", "postgres", false},
		{"mysql is valid", "mysql", false},
		{"mariadb is valid", "mariadb", false},
		{"sqlite is valid", "sqlite", false},
		{"oracle is valid", "oracle", false},
		{"case insensitive", "PostgreS", false},
		{"invalid type", "invalid", true},
		{"mongodb is valid", "mongodb", false},
		{"mssql is valid", "mssql", false},
		{"snowflake is valid", "snowflake", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDBType(tt.dbType)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDBType(%q) error = %v, wantErr %v", tt.dbType, err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "unsupported database type") {
				t.Errorf("ValidateDBType(%q) error message should contain 'unsupported database type', got %v", tt.dbType, err)
			}
		})
	}
}

func TestValidateMultiDBType(t *testing.T) {
	tests := []struct {
		name    string
		dbType  string
		wantErr bool
	}{
		{"empty string defaults to postgres", "", false},
		{"postgres is valid", "postgres", false},
		{"mysql is valid", "mysql", false},
		{"mariadb is valid", "mariadb", false},
		{"sqlite is valid", "sqlite", false},
		{"oracle is valid", "oracle", false},
		{"mongodb is valid for multi-db", "mongodb", false},
		{"mssql is valid for multi-db", "mssql", false},
		{"snowflake is valid for multi-db", "snowflake", false},
		{"case insensitive", "PostgreS", false},
		{"invalid type", "invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMultiDBType(tt.dbType)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMultiDBType(%q) error = %v, wantErr %v", tt.dbType, err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeMode(t *testing.T) {
	tests := []struct {
		name           string
		mode           string
		production     bool
		wantMode       string
		wantProduction bool
		wantErr        bool
	}{
		{name: "empty dev", wantMode: "dev"},
		{name: "empty prod from production", production: true, wantMode: "prod", wantProduction: true},
		{name: "dev", mode: "dev", production: true, wantMode: "dev", wantProduction: true},
		{name: "prod", mode: "prod", wantMode: "prod"},
		{name: "agentic", mode: "agentic", wantMode: "agentic"},
		{name: "invalid", mode: "secure-ish", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := Config{Mode: tt.mode, Production: tt.production}
			err := conf.NormalizeMode()
			if (err != nil) != tt.wantErr {
				t.Fatalf("NormalizeMode() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if conf.Mode != tt.wantMode || conf.Production != tt.wantProduction {
				t.Fatalf("NormalizeMode() = mode %q production %v, want mode %q production %v",
					conf.Mode, conf.Production, tt.wantMode, tt.wantProduction)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty config is valid",
			config:  Config{},
			wantErr: false,
		},
		{
			name:    "valid postgres config",
			config:  Config{DBType: "postgres"},
			wantErr: false,
		},
		{
			name:    "invalid primary db type",
			config:  Config{DBType: "invalid"},
			wantErr: true,
			errMsg:  "unsupported database type",
		},
		{
			name: "valid multi-database config",
			config: Config{
				DBType: "postgres",
				Databases: map[string]DatabaseConfig{
					"secondary": {Type: "mysql"},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid multi-database type",
			config: Config{
				DBType: "postgres",
				Databases: map[string]DatabaseConfig{
					"secondary": {Type: "invalid"},
				},
			},
			wantErr: true,
			errMsg:  "database \"secondary\"",
		},
		{
			name: "mongodb valid in multi-db",
			config: Config{
				DBType: "postgres",
				Databases: map[string]DatabaseConfig{
					"mongo": {Type: "mongodb"},
				},
			},
			wantErr: false,
		},
		{
			name: "snowflake valid in multi-db",
			config: Config{
				DBType: "postgres",
				Databases: map[string]DatabaseConfig{
					"snowflake": {Type: "snowflake"},
				},
			},
			wantErr: false,
		},
		{
			name: "legacy codesql database rejected",
			config: Config{
				Databases: map[string]DatabaseConfig{"code": {Type: "codesql"}},
			},
			wantErr: true,
			errMsg:  "kind: code",
		},
		{
			name: "sources used rejects legacy databases",
			config: Config{
				Sources:   []SourceConfig{{Name: "app", Kind: "database", Type: "postgres"}},
				Databases: map[string]DatabaseConfig{"app": {Type: "postgres"}},
			},
			wantErr: true,
			errMsg:  "databases is legacy",
		},
		{
			name: "empty sources still selects sources used",
			config: Config{
				Sources:   []SourceConfig{},
				Databases: map[string]DatabaseConfig{"app": {Type: "postgres"}},
			},
			wantErr: true,
			errMsg:  "databases is legacy",
		},
		{
			name: "sources used requires table source",
			config: Config{
				Sources: []SourceConfig{{Name: "app", Kind: "database", Type: "postgres"}},
				Tables:  []Table{{Name: "users"}},
			},
			wantErr: true,
			errMsg:  "source is required",
		},
		{
			name: "sources used rejects table database",
			config: Config{
				Sources: []SourceConfig{{Name: "app", Kind: "database", Type: "postgres"}},
				Tables:  []Table{{Name: "users", Source: "app", Database: "app"}},
			},
			wantErr: true,
			errMsg:  "database is legacy",
		},
		{
			name: "sources used accepts valid capabilities",
			config: Config{
				Sources: []SourceConfig{{
					Name:         "graphjin",
					Kind:         "graphjin",
					Capabilities: map[string]bool{"security.read": true},
				}},
			},
			wantErr: false,
		},
		{
			name: "sources used rejects unknown capability",
			config: Config{
				Sources: []SourceConfig{{
					Name:         "graphjin",
					Kind:         "graphjin",
					Capabilities: map[string]bool{"security.audit": true},
				}},
			},
			wantErr: true,
			errMsg:  "supported: catalog.read",
		},
		{
			name: "sources used rejects legacy role table access",
			config: Config{
				Sources: []SourceConfig{{Name: "app", Kind: "database", Type: "postgres"}},
				Roles:   []Role{{Name: "user", Tables: []RoleTable{{Name: "users"}}}},
			},
			wantErr: true,
			errMsg:  `query_catalog(search: "migrate legacy roles tables to source access")`,
		},
		{
			name: "sources used rejects conflicting identity query aliases",
			config: Config{
				Sources:    []SourceConfig{{Name: "app", Kind: "database", Type: "postgres"}},
				RolesQuery: `SELECT * FROM legacy_roles WHERE id = $user_id`,
				Identity:   IdentityConfig{Query: `SELECT * FROM source_roles WHERE id = $user_id`},
			},
			wantErr: true,
			errMsg:  "identity.query and roles_query are aliases",
		},
		{
			name: "sources used rejects public write",
			config: Config{
				Sources: []SourceConfig{{Name: "app", Kind: "database", Type: "postgres", Access: SourceAccessConfig{Write: "public"}}},
			},
			wantErr: true,
			errMsg:  "public write",
		},
		{
			name: "sources used rejects artifact source on non database",
			config: Config{
				Sources:   []SourceConfig{{Name: "code", Kind: "code", Type: "sqlite"}},
				Artifacts: ArtifactsConfig{Enabled: true, Source: "code"},
			},
			wantErr: true,
			errMsg:  "writable SQL database source",
		},
		{
			name: "sources used rejects artifact source on mongodb",
			config: Config{
				Sources:   []SourceConfig{{Name: "mongo", Kind: "database", Type: "mongodb"}},
				Artifacts: ArtifactsConfig{Enabled: true, Source: "mongo"},
			},
			wantErr: true,
			errMsg:  "writable SQL database source",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Config.Validate() error = %v, should contain %q", err, tt.errMsg)
			}
		})
	}
}

func TestNormalizeSourcesAppliesIdentityAccessAndArtifactDefaults(t *testing.T) {
	conf := &Config{
		RolesQuery: `SELECT * FROM users WHERE id = $user_id`,
		Sources: []SourceConfig{
			{Name: "app", Kind: "database", Type: "postgres", Default: true},
			{Name: "graphjin", Kind: "graphjin"},
		},
		Artifacts: ArtifactsConfig{Enabled: true},
	}
	if err := conf.NormalizeSources(); err != nil {
		t.Fatalf("NormalizeSources: %v", err)
	}
	if conf.Identity.UserIDClaim != "sub" || conf.Identity.NamespaceClaim != "account_id" || conf.Identity.Query != conf.RolesQuery {
		t.Fatalf("identity defaults not applied: %+v", conf.Identity)
	}
	if conf.Artifacts.Source != "app" || conf.Artifacts.Schema != "_graphjin" || conf.Artifacts.GlobalsPath != "./config" || !conf.Artifacts.AutoInitEnabled() {
		t.Fatalf("artifact defaults not applied: %+v", conf.Artifacts)
	}
	app, _ := conf.SourceByName("app")
	if app.Access.Read != AccessModeAccount || app.Access.Write != AccessModeBlocked || app.Access.Delete != AccessModeBlocked ||
		app.Access.NamespaceColumn != "account_id" || app.Access.MissingNamespaceColumn != MissingNamespaceBlock {
		t.Fatalf("database access defaults not applied: %+v", app.Access)
	}
	gj, _ := conf.SourceByName("graphjin")
	if gj.Access.Roots["gj_security"] != AccessModeAdmin || gj.Access.Roots["gj_runtime"] != AccessModeAdmin ||
		gj.Access.Roots["gj_artifacts"] != AccessModeAccount {
		t.Fatalf("graphjin root access defaults not applied: %+v", gj.Access.Roots)
	}
}

func TestNormalizeSourcesTreatsIdentityQueryAsV1LiteRolesQueryAlias(t *testing.T) {
	sourceMode := []SourceConfig{{Name: "app", Kind: "database", Type: "postgres", Default: true}}

	t.Run("identity query populates roles query", func(t *testing.T) {
		conf := &Config{
			Sources:  sourceMode,
			Identity: IdentityConfig{Query: `SELECT * FROM roles WHERE id = $user_id`},
		}
		if err := conf.NormalizeSources(); err != nil {
			t.Fatalf("NormalizeSources: %v", err)
		}
		if conf.RolesQuery != conf.Identity.Query {
			t.Fatalf("identity.query should normalize to roles_query in V1-lite mode: identity=%q roles_query=%q",
				conf.Identity.Query, conf.RolesQuery)
		}
	})

	t.Run("roles query populates identity query", func(t *testing.T) {
		conf := &Config{
			Sources:    sourceMode,
			RolesQuery: `SELECT * FROM roles WHERE id = $user_id`,
		}
		if err := conf.NormalizeSources(); err != nil {
			t.Fatalf("NormalizeSources: %v", err)
		}
		if conf.Identity.Query != conf.RolesQuery {
			t.Fatalf("roles_query should remain a deprecated identity.query alias in source mode: identity=%q roles_query=%q",
				conf.Identity.Query, conf.RolesQuery)
		}
	})
}

func TestNormalizeSourcesMapsSourcesAndRelationships(t *testing.T) {
	conf := &Config{
		Sources: []SourceConfig{
			{Name: "app", Kind: "database", Type: "postgres", Default: true},
			{Name: "code", Kind: "code", Path: "/src", ReadOnly: true},
			{Name: "avatars", Kind: "file", Backend: "local", Root: "/tmp/avatars"},
			{Name: "graphjin", Kind: "graphjin"},
			{Name: "workflows", Kind: "workflow", ReadOnly: true},
		},
		Tables: []Table{
			{Name: "users", Source: "app"},
			{Name: "code_files", Source: "code"},
			{Name: "avatars", Source: "avatars"},
			{Name: "gj_workflow", Source: "workflows"},
		},
		Relationships: []RelationshipConfig{{From: "users.id", To: "code:code_db_refs.table_key"}},
	}
	if err := conf.NormalizeSources(); err != nil {
		t.Fatalf("NormalizeSources: %v", err)
	}
	if conf.Databases["app"].Type != "postgres" || conf.Databases["code"].Type != "codesql" {
		t.Fatalf("unexpected database normalization: %+v", conf.Databases)
	}
	if len(conf.Filesystems) != 1 || conf.Filesystems[0].Name != "avatars" {
		t.Fatalf("unexpected filesystem normalization: %+v", conf.Filesystems)
	}
	if conf.Tables[0].Database != "app" || conf.Tables[1].Database != "code" || conf.Tables[2].Database != "app" {
		t.Fatalf("unexpected table database mapping: %+v", conf.Tables)
	}
	if !conf.Tables[1].ReadOnly || !conf.Tables[3].ReadOnly {
		t.Fatalf("source read_only was not applied to tables: %+v", conf.Tables)
	}
	if len(conf.Tables[0].Columns) != 1 || conf.Tables[0].Columns[0].ForeignKey != "code:code_db_refs.table_key" {
		t.Fatalf("relationship overlay not applied: %+v", conf.Tables[0].Columns)
	}
}

func TestRenormalizeSourcesRebuildsGeneratedRuntimeFields(t *testing.T) {
	conf := &Config{
		Sources: []SourceConfig{
			{Name: "app", Kind: "database", Type: "sqlite", Path: "old.sqlite3", Default: true},
			{Name: "graphjin", Kind: "graphjin"},
		},
		Tables: []Table{{Name: "users", Source: "app"}},
	}
	if err := conf.NormalizeSources(); err != nil {
		t.Fatalf("NormalizeSources: %v", err)
	}
	if got := conf.Databases["app"].Path; got != "old.sqlite3" {
		t.Fatalf("normalized database path = %q", got)
	}
	if got := conf.Tables[0].Database; got != "app" {
		t.Fatalf("normalized table database = %q", got)
	}

	conf.Sources[0].Path = "new.sqlite3"
	if err := conf.RenormalizeSources(); err != nil {
		t.Fatalf("RenormalizeSources: %v", err)
	}
	if got := conf.Databases["app"].Path; got != "new.sqlite3" {
		t.Fatalf("renormalized database path = %q", got)
	}
	if got := conf.Tables[0].Database; got != "app" {
		t.Fatalf("renormalized table database = %q", got)
	}
}
