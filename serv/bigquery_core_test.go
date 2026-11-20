package serv

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/dosco/graphjin/bigquerydriver"
	"github.com/dosco/graphjin/bigquerydriver/bqtest"
	"github.com/dosco/graphjin/core/v3"
	corediscovery "github.com/dosco/graphjin/core/v3/discovery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	bq "google.golang.org/api/bigquery/v2"
	"google.golang.org/api/option"
)

// TestCoreBigQuerySQLRunsThroughTheDriver sends the SQL core writes for a
// BigQuery source — schema discovery, row counts and a compiled GraphQL query —
// through the production driver to a stand-in that applies BigQuery's own
// request rules.
//
// The BigQuery simulator the other suites use accepts SQL that BigQuery would
// refuse (a bare INFORMATION_SCHEMA view, for one), and it is opened directly,
// without this driver. So a change in core that breaks a real BigQuery source
// passes those suites. It fails here: the stand-in refuses the statement, or
// has no reply for a statement core did not send before. If that happens,
// check the new SQL against a real project (tests/bigquerylive) before
// adjusting the replies below.
func TestCoreBigQuerySQLRunsThroughTheDriver(t *testing.T) {
	fake := bqtest.New(t)

	// The twelve columns every catalog query returns.
	catalog := []*bq.TableFieldSchema{
		bqtest.Field("schema_name", "STRING"), bqtest.Field("table_name", "STRING"),
		bqtest.Field("column_name", "STRING"), bqtest.Field("col_type", "STRING"),
		bqtest.Field("not_null", "BOOLEAN"), bqtest.Field("primary_key", "BOOLEAN"),
		bqtest.Field("unique_key", "BOOLEAN"), bqtest.Field("is_array", "BOOLEAN"),
		bqtest.Field("full_text", "BOOLEAN"), bqtest.Field("foreignkey_schema", "STRING"),
		bqtest.Field("foreignkey_table", "STRING"), bqtest.Field("foreignkey_column", "STRING"),
	}
	column := func(table, name, typ string, notNull bool) []any {
		return []any{"analytics", table, name, typ, notNull, false, false, false, false, "", "", ""}
	}
	key := func(table, name string) []any {
		return []any{"analytics", table, name, "", false, true, false, false, false, "", "", ""}
	}

	// Each reply is keyed on text the driver must have produced: the dataset
	// and region qualifiers BigQuery requires.
	const dataset = "`acme-prod`.`analytics`.INFORMATION_SCHEMA."
	fake.OnContains("AS db_version", bqtest.Reply{
		Fields: []*bq.TableFieldSchema{bqtest.Field("db_version", "INTEGER"), bqtest.Field("db_schema", "STRING"), bqtest.Field("db_name", "STRING")},
		Rows:   [][]any{{int64(0), "analytics", "acme-prod"}},
	})
	fake.OnContains("AS constraint_count", bqtest.Reply{
		Fields: []*bq.TableFieldSchema{bqtest.Field("constraint_count", "INTEGER")},
		Rows:   [][]any{{int64(3)}},
	})
	fake.OnContains("STRING_AGG(fk_kcu.column_name", bqtest.Reply{ // composite foreign keys: none
		Fields: []*bq.TableFieldSchema{
			bqtest.Field("table_schema", "STRING"), bqtest.Field("table_name", "STRING"), bqtest.Field("constraint_name", "STRING"),
			bqtest.Field("local_columns", "STRING"), bqtest.Field("fkey_schema", "STRING"), bqtest.Field("fkey_table", "STRING"),
			bqtest.Field("fkey_columns", "STRING"),
		},
	})
	fake.OnContains("FROM "+dataset+"KEY_COLUMN_USAGE fk_kcu", bqtest.Reply{
		Fields: catalog,
		Rows:   [][]any{{"analytics", "orders", "user_id", "", false, false, false, false, false, "analytics", "users", "id"}},
	})
	fake.OnContains("FROM "+dataset+"KEY_COLUMN_USAGE kcu", bqtest.Reply{
		Fields: catalog,
		Rows:   [][]any{key("users", "id"), key("orders", "id")},
	})
	fake.OnContains("FROM "+dataset+"COLUMNS col", bqtest.Reply{
		Fields: catalog,
		Rows: [][]any{
			column("users", "id", "int64", true), column("users", "email", "string", true),
			column("orders", "id", "int64", true), column("orders", "user_id", "int64", true),
			column("orders", "total", "numeric", false),
		},
	})
	fake.OnContains("FROM `acme-prod`.`region-europe-west2`.INFORMATION_SCHEMA.TABLE_STORAGE", bqtest.Reply{
		Fields: []*bq.TableFieldSchema{bqtest.Field("TOTAL_ROWS", "INTEGER")},
		Rows:   [][]any{{int64(42)}},
	})
	const answer = `{"users":[{"id":2,"email":"grace@example.com","orders":[{"id":7,"total":12.5}]}]}`
	fake.OnContains("TO_JSON_STRING(JSON_OBJECT(", bqtest.Reply{
		Fields: []*bq.TableFieldSchema{bqtest.Field("__root", "STRING")},
		Rows:   [][]any{{answer}},
	})

	connector, err := bigquerydriver.NewConnector(context.Background(), bigquerydriver.Config{
		ProjectID: fake.Project,
		DatasetID: fake.Dataset,
		ClientOptions: []option.ClientOption{
			option.WithEndpoint(fake.Endpoint()),
			option.WithoutAuthentication(),
		},
	})
	require.NoError(t, err)
	db := sql.OpenDB(connector)
	defer db.Close() //nolint:errcheck

	// An empty folder of its own: core caches a discovered schema on disk and
	// falls back to that cache when discovery fails, which would hide exactly
	// the failure this test exists to catch.
	gj, err := core.NewGraphJinWithFS(&core.Config{DBType: "bigquery", DisableAllowList: true}, db, core.NewOsFS(t.TempDir()))
	require.NoError(t, err, "schema discovery through the driver")
	defer gj.Close()

	users, err := gj.GetTableSchema("users")
	require.NoError(t, err)
	assert.Equal(t, "id", users.PrimaryKey)

	// A relationship (found through the foreign key) and a variable, which
	// must reach BigQuery as a positional parameter.
	res, err := gj.GraphQL(context.Background(), `query {
		users(where: { id: { gt: $after } }, order_by: { id: asc }, limit: 2) {
			id
			email
			orders { id total }
		}
	}`, json.RawMessage(`{"after": 1}`), nil)
	require.NoError(t, err)
	assert.JSONEq(t, answer, string(res.Data))
	query := fake.LastQuery(t)
	assert.Equal(t, "POSITIONAL", query["parameterMode"], "the variable was not bound as a parameter:\n%s", query["query"])

	rows, ok := corediscovery.ApproxRowCount(context.Background(), db, "bigquery", users)
	assert.True(t, ok, "row count lookup")
	assert.Equal(t, int64(42), rows)

	// Core drops some discovery errors on purpose, so ask the stand-in too.
	for _, refused := range fake.Refused() {
		t.Errorf("BigQuery would refuse a statement core sent: %s\nSQL: %s", refused.Message, refused.SQL)
	}
}
