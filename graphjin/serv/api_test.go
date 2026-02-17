package serv

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zaptest"
)

func TestNormalStart_NilDBInDevMode(t *testing.T) {
	logger := zaptest.NewLogger(t)
	s := &graphjinService{
		dbs: make(map[string]*sql.DB),
		conf: &Config{
			Serv: Serv{Production: false}, // dev mode
		},
		log:  logger.Sugar(),
		zlog: logger,
	}

	err := s.normalStart()
	assert.NoError(t, err)
	assert.Nil(t, s.gj) // gj should remain nil
}

// Note: Testing production mode with nil DB would require mocking the database
// connection, which is complex. The key behavior difference is tested above:
// in dev mode, normalStart returns early with no error when DB is nil.
// In production mode, the function would proceed to NewGraphJin and fail.
