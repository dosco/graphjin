package tests_test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/require"
)

func snowflakeDebugEnabled() bool {
	return os.Getenv("GRAPHJIN_SNOWFLAKE_DEBUG") == "1"
}

func logSnowflakeQueryFailure(gj *core.GraphJin, gql string, vars json.RawMessage, err error) {
	if dbType != "snowflake" || err == nil || gj == nil {
		return
	}

	fmt.Fprintf(os.Stderr, "\n[Snowflake debug] GraphQL execution error: %v\n", err)
	if len(vars) != 0 {
		fmt.Fprintf(os.Stderr, "[Snowflake debug] Variables: %s\n", string(vars))
	}

	exp, expErr := gj.ExplainQuery(gql, vars, "user")
	if expErr != nil {
		fmt.Fprintf(os.Stderr, "[Snowflake debug] ExplainQuery failed: %v\n", expErr)
		return
	}

	fmt.Fprintf(os.Stderr, "[Snowflake debug] Compiled SQL:\n%s\n", exp.CompiledQuery)
}

func execSchemaDiffSQL(t *testing.T, sql string) {
	t.Helper()

	if dbType == "snowflake" && snowflakeDebugEnabled() {
		t.Logf("[Snowflake debug] schema diff SQL:\n%s", sql)
	}

	_, err := db.Exec(sql)
	if err != nil && dbType == "snowflake" {
		t.Logf("[Snowflake debug] schema diff exec failed: %v", err)
		t.Logf("[Snowflake debug] SQL:\n%s", sql)
	}
	require.NoError(t, err, "failed to execute: %s", sql)
}
