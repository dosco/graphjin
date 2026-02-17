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
