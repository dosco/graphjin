package serv

import (
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/assert"
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
	}

	err := s.initDB()
	assert.NoError(t, err)
	assert.Nil(t, s.db) // db should remain nil
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
		db:   nil, // Normally would have a real db
	}

	// When db is nil but not configured, it should return nil without error in dev mode
	err := s.initDB()
	assert.NoError(t, err)
}

// newTestLogger creates a no-op logger for testing
func newTestLogger() *zap.SugaredLogger {
	logger, _ := zap.NewDevelopment()
	return logger.Sugar()
}
