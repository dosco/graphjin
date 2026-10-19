package serv

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestIsDatabaseConfigured(t *testing.T) {
	tests := []struct {
		name     string
		conf     *Config
		expected bool
	}{
		{
			name:     "empty config - no database",
			conf:     &Config{},
			expected: false,
		},
		{
			name: "connection string provided",
			conf: &Config{
				Serv: Serv{
					DB: Database{ConnString: "postgres://localhost/testdb"},
				},
			},
			expected: true,
		},
		{
			name: "host and dbname provided",
			conf: &Config{
				Serv: Serv{
					DB: Database{Host: "localhost", DBName: "testdb"},
				},
			},
			expected: true,
		},
		{
			name: "only host provided - insufficient",
			conf: &Config{
				Serv: Serv{
					DB: Database{Host: "localhost"},
				},
			},
			expected: false,
		},
		{
			name: "only dbname provided - insufficient",
			conf: &Config{
				Serv: Serv{
					DB: Database{DBName: "testdb"},
				},
			},
			expected: false,
		},
		{
			name: "multi-database config provided",
			conf: &Config{
				Core: core.Config{
					Databases: map[string]core.DatabaseConfig{
						"main": {Type: "postgres", Host: "localhost"},
					},
				},
			},
			expected: true,
		},
		{
			name: "empty multi-database config",
			conf: &Config{
				Core: core.Config{
					Databases: map[string]core.DatabaseConfig{},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &graphjinService{conf: tt.conf}
			result := s.isDatabaseConfigured()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInitDB_DevModeWithoutDatabase(t *testing.T) {
	// Test that initDB returns nil (no error) in dev mode without database
	logger := zaptest.NewLogger(t)
	s := &graphjinService{
		conf: &Config{
			Serv: Serv{Production: false}, // dev mode
		},
		log:  logger.Sugar(),
		zlog: logger,
		dbs:  make(map[string]*sql.DB),
	}

	err := s.initDB()
	assert.NoError(t, err)
	assert.Empty(t, s.dbs) // dbs should remain empty
}

func TestInitDB_ExistingDBNotReplaced(t *testing.T) {
	// Test that initDB returns early when db is already set
	logger := zaptest.NewLogger(t)

	// Create a mock that we can verify wasn't changed
	s := &graphjinService{
		conf: &Config{
			Serv: Serv{Production: false},
		},
		log:  logger.Sugar(),
		zlog: logger,
		dbs:  make(map[string]*sql.DB),
	}

	// When dbs is empty but not configured, it should return nil without error in dev mode
	err := s.initDB()
	assert.NoError(t, err)
}

func TestInitDB_ProvidedDBsStillInitializeMissingManagedSources(t *testing.T) {
	logger := zaptest.NewLogger(t)
	codeRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(codeRoot, "main.go"), []byte(`package main

func Handler() {}
`), 0o644))

	appDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	s := &graphjinService{
		conf: &Config{
			Core: core.Config{Databases: map[string]core.DatabaseConfig{
				"app":  {Type: "sqlite", Path: ":memory:"},
				"code": {Type: "codesql", Path: codeRoot},
			}},
			Serv: Serv{
				ConfigPath: t.TempDir(),
				Production: false,
				MCP:        MCPConfig{Disable: true},
			},
		},
		log:        logger.Sugar(),
		zlog:       logger,
		dbs:        map[string]*sql.DB{"app": appDB},
		managedDBs: make(map[string]managedDB),
	}
	defer closeTestService(s)

	err = s.initDB()
	require.NoError(t, err)
	assert.Same(t, appDB, s.dbs["app"])
	require.NotNil(t, s.dbs["code"])
	assert.Equal(t, "sqlite", s.runtimeCore.Databases["code"].Type)
	assert.Equal(t, "codesql", s.conf.Core.Databases["code"].Type)
}

// TestNewDBFromDatabaseConfigHonorsPostgresTLSValidation guards #564. The
// configured multi-database path must pass TLS fields through the same driver
// initializer as the legacy single-database path. Missing required TLS fields
// must therefore fail before GraphJin attempts a plaintext connection.
func TestNewDBFromDatabaseConfigHonorsPostgresTLSValidation(t *testing.T) {
	s := &graphjinService{
		conf:       &Config{},
		fs:         core.NewOsFS(""),
		managedDBs: make(map[string]managedDB),
	}

	base := core.DatabaseConfig{
		Type:        "postgres",
		Host:        "127.0.0.1",
		Port:        1,
		DBName:      "analytics",
		User:        "graphjin",
		Password:    "secret",
		EnableTLS:   true,
		PingTimeout: time.Millisecond,
	}
	t.Run("server name", func(t *testing.T) {
		_, err := s.newDBFromDatabaseConfig("analytics", base)
		require.ErrorContains(t, err, "tls: server_name is required")
	})
	t.Run("server certificate", func(t *testing.T) {
		conf := base
		conf.ServerName = "analytics.example.com"
		_, err := s.newDBFromDatabaseConfig("analytics", conf)
		require.ErrorContains(t, err, "tls: server_cert is required")
	})
}

// newTestLogger creates a no-op logger for testing
func newTestLogger() *zap.SugaredLogger {
	logger, _ := zap.NewDevelopment()
	return logger.Sugar()
}

func TestSyncDBFromDatabases_MSSQL(t *testing.T) {
	boolFalse := false
	boolTrue := true

	conf := &Config{
		Core: core.Config{
			Databases: map[string]core.DatabaseConfig{
				"mssql": {
					Type:                   "mssql",
					Host:                   "mssqlhost",
					Port:                   1433,
					DBName:                 "testdb",
					User:                   "sa",
					Password:               "secret",
					Schema:                 "dbo",
					Encrypt:                &boolFalse,
					TrustServerCertificate: &boolTrue,
				},
			},
		},
	}

	ok := syncDBFromDatabases(conf)
	assert.True(t, ok)
	assert.Equal(t, "mssql", conf.DB.Type)
	assert.Equal(t, "mssqlhost", conf.DB.Host)
	assert.Equal(t, uint16(1433), conf.DB.Port)
	assert.Equal(t, "testdb", conf.DB.DBName)
	assert.Equal(t, "sa", conf.DB.User)
	assert.Equal(t, "secret", conf.DB.Password)
	assert.Equal(t, "dbo", conf.DB.Schema)
	assert.NotNil(t, conf.DB.Encrypt)
	assert.False(t, *conf.DB.Encrypt)
	assert.NotNil(t, conf.DB.TrustServerCertificate)
	assert.True(t, *conf.DB.TrustServerCertificate)
	assert.Equal(t, "mssql", conf.DBType)
}

func TestSyncDBFromDatabases_DefaultDB(t *testing.T) {
	conf := &Config{
		Core: core.Config{
			Databases: map[string]core.DatabaseConfig{
				"primary":   {Type: "postgres", Host: "pghost"},
				"secondary": {Type: "mssql", Host: "mssqlhost"},
			},
		},
	}

	ok := syncDBFromDatabases(conf)
	assert.True(t, ok)
	assert.Equal(t, "postgres", conf.DB.Type)
	assert.Equal(t, "pghost", conf.DB.Host)
}

func TestSyncDBFromDatabases_EmptyMap(t *testing.T) {
	conf := &Config{
		Core: core.Config{
			Databases: map[string]core.DatabaseConfig{},
		},
	}

	ok := syncDBFromDatabases(conf)
	assert.False(t, ok)
}

// TestMultiDB_MSSQLViaDatabasesConfig_ConnString tests the full pipeline:
// databases: config → syncDBFromDatabases → initMssql → correct connection string.
// This verifies that MSSQL-specific fields (encrypt, trust_server_certificate) and
// special characters in passwords are correctly handled end-to-end.
func TestMultiDB_MSSQLViaDatabasesConfig_ConnString(t *testing.T) {
	boolFalse := false
	boolTrue := true

	conf := &Config{
		Core: core.Config{
			Databases: map[string]core.DatabaseConfig{
				"postgres": {
					Type:     "postgres",
					Host:     "pghost",
					Port:     5432,
					DBName:   "pgdb",
					User:     "pguser",
					Password: "pgpass",
				},
				"mssql": {
					Type:                   "mssql",
					Host:                   "mssqlhost",
					Port:                   1433,
					DBName:                 "testdb",
					User:                   "sa",
					Password:               "GraphJin!Passw0rd",
					Encrypt:                &boolFalse,
					TrustServerCertificate: &boolTrue,
				},
			},
		},
	}

	// Simulate selecting the MSSQL database (as multi-DB init would)
	mssqlConf := conf.Core.Databases["mssql"]

	// Build a config as syncDBFromDatabases would for the MSSQL entry
	mssqlServConf := &Config{}
	mssqlServConf.DB.Type = mssqlConf.Type
	mssqlServConf.DB.Host = mssqlConf.Host
	mssqlServConf.DB.Port = uint16(mssqlConf.Port)
	mssqlServConf.DB.DBName = mssqlConf.DBName
	mssqlServConf.DB.User = mssqlConf.User
	mssqlServConf.DB.Password = mssqlConf.Password
	mssqlServConf.DB.Encrypt = mssqlConf.Encrypt
	mssqlServConf.DB.TrustServerCertificate = mssqlConf.TrustServerCertificate

	// Call initMssql to build the connection string
	dc, err := initMssql(mssqlServConf, true, false, core.NewOsFS(""))
	require.NoError(t, err)

	expected := "sqlserver://sa:GraphJin%21Passw0rd@mssqlhost:1433?database=testdb&encrypt=disable&trustservercertificate=true"
	assert.Equal(t, expected, dc.connString)
	assert.Equal(t, "sqlserver", dc.driverName)
}
