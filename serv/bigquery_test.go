package serv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTestServiceAccountKey writes a service-account key file the Google
// client accepts. It lets these tests build a connector on a machine with no
// Google credentials, and nothing is sent anywhere: a token is only requested
// when the first query runs.
func writeTestServiceAccountKey(t *testing.T, dir, name string) string {
	t.Helper()
	key, err := json.Marshal(map[string]string{
		"type":           "service_account",
		"project_id":     "acme-prod",
		"private_key_id": "test",
		"private_key":    string(generateTestRSAKeyPEM(t)),
		"client_email":   "graphjin@acme-prod.iam.gserviceaccount.com",
		"client_id":      "1",
		"token_uri":      "https://oauth2.googleapis.com/token",
	})
	require.NoError(t, err)
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, key, 0o600))
	return path
}

func TestInitDBDriver_BigQuery(t *testing.T) {
	dir := t.TempDir()
	keyPath := writeTestServiceAccountKey(t, dir, "sa.json")
	connString := "bigquery://acme-prod/analytics?location=EU&credentials_file=" + keyPath

	t.Run("type bigquery", func(t *testing.T) {
		conf := &Config{Serv: Serv{DB: Database{Type: "bigquery", ConnString: connString}}}
		dc, err := initDBDriver(conf, true, false, core.NewOsFS(dir))
		require.NoError(t, err)
		assert.Equal(t, "bigquery", conf.DBType)
		assert.Equal(t, "bigquery", dc.driverName)
		assert.NotNil(t, dc.connector, "bigquery opens through a connector, not a driver name")
	})

	t.Run("type left out", func(t *testing.T) {
		conf := &Config{Serv: Serv{DB: Database{ConnString: connString}}}
		dc, err := initDBDriver(conf, true, false, core.NewOsFS(dir))
		require.NoError(t, err)
		assert.Equal(t, "bigquery", conf.DBType, "a bigquery:// connection string names the type")
		assert.NotNil(t, dc.connector)
	})

	t.Run("entry under databases", func(t *testing.T) {
		conf := configuredDatabaseServiceConfig("bigquery", core.DatabaseConfig{Type: "bigquery", ConnString: connString})
		dc, err := initDBDriver(conf, false, false, core.NewOsFS(dir))
		require.NoError(t, err)
		assert.Equal(t, "bigquery", conf.DBType)
		assert.NotNil(t, dc.connector)
	})

	t.Run("key file relative to the config folder", func(t *testing.T) {
		conf := &Config{Serv: Serv{DB: Database{
			Type:       "bigquery",
			ConnString: "bigquery://acme-prod/analytics?credentials_file=sa.json",
		}}}
		// The working directory is not dir, so this only passes if the file
		// is read from the config filesystem.
		dc, err := initDBDriver(conf, true, false, core.NewOsFS(dir))
		require.NoError(t, err)
		assert.NotNil(t, dc.connector)
	})
}

func TestInitDBDriver_BigQueryConfigErrors(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		db   Database
		want string
	}{
		{
			name: "no connection string",
			db:   Database{Type: "bigquery", Host: "bigquery.googleapis.com", DBName: "analytics"},
			want: "bigquery requires connection_string: bigquery://project/dataset",
		},
		{
			name: "no dataset",
			db:   Database{Type: "bigquery", ConnString: "bigquery://acme-prod"},
			want: "dataset id is required",
		},
		{
			name: "misspelled option",
			db:   Database{Type: "bigquery", ConnString: "bigquery://acme-prod/analytics?locaton=EU"},
			want: `unknown connection string option "locaton"`,
		},
		{
			name: "missing key file",
			db:   Database{Type: "bigquery", ConnString: "bigquery://acme-prod/analytics?credentials_file=absent.json"},
			want: `reading credentials_file "absent.json"`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := initDBDriver(&Config{Serv: Serv{DB: c.db}}, true, false, core.NewOsFS(dir))
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.want)
		})
	}
}

func TestDiscoverySampleSQL_BigQueryUsesBackticks(t *testing.T) {
	schema := &core.TableSchema{Schema: "analytics", Name: "orders"}
	ref := qualifiedTable("bigquery", schema)
	assert.Equal(t, "`analytics`.`orders`", ref)
	assert.Equal(t,
		"SELECT `id`, `placed at` FROM `analytics`.`orders` LIMIT 5",
		buildSampleQuery("bigquery", quoteIdent("bigquery", "id")+", "+quoteIdent("bigquery", "placed at"), ref, 5))
	// Other engines keep the standard double quotes.
	assert.Equal(t, `"analytics"."orders"`, qualifiedTable("postgres", schema))
}
