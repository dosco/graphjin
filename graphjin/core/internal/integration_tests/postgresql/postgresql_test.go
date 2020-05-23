package cockraochdb_test

import (
	"database/sql"
	"os"
	"testing"

	integration_tests "github.com/dosco/super-graph/core/internal/integration_tests"
	_ "github.com/jackc/pgx/v4/stdlib"
	"github.com/stretchr/testify/require"
)

func TestCockroachDB(t *testing.T) {

	url, found := os.LookupEnv("SG_POSTGRESQL_TEST_URL")
	if !found {
		t.Skip("set the SG_POSTGRESQL_TEST_URL env variable if you want to run integration tests against a PostgreSQL database")
	} else {
		db, err := sql.Open("pgx", url)
		require.NoError(t, err)

		integration_tests.DropSchema(t, db)
		integration_tests.SetupSchema(t, db)
		integration_tests.TestSuperGraph(t, db, func(t *testing.T) {
		})
	}
}
