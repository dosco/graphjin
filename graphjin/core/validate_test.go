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
