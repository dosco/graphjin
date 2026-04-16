package tests_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/snowflakedb/gosnowflake"
)

// Silence the gosnowflake driver's own logrus logger for the test run.
// It logs every failed query at ERRO level — including probes the
// caller intentionally expects to fail (e.g. the `_gj_fk_metadata`
// overlay probe during schema discovery). Keeping these at ERRO floods
// test output with alarming-looking errors that are actually handled
// gracefully by GraphJin. The `serv` package does the same for its
// binary startup path (see serv/db.go silenceSnowflakeDriverLogs).
func init() {
	_ = gosnowflake.GetLogger().SetLogLevel("fatal")
}

// startSnowflake is the integration-test harness entry point for Snowflake.
// It follows the same shape as the other per-DB startFuncs in dbint_test.go
// (return: cleanup, DSN, err) but does NOT use testcontainers — real
// Snowflake can't be run locally. Instead it reads SNOWFLAKE_TEST_CONN from
// the environment, connects, creates an ephemeral test schema, runs the
// standard ./snowflake.sql seed, and returns a cleanup func that drops the
// schema.
//
// The DSN it returns has the ephemeral schema substituted in so subsequent
// test code that reopens the connection lands in the right schema.
//
// Expected DSN format (matches gosnowflake):
//
//	USER:PASS@ACCOUNT/DATABASE?warehouse=WH&role=ROLE
//
// Schema is omitted — the harness manages it. A connection string that
// already contains `/SCHEMA` is still accepted; the supplied schema is
// replaced with the ephemeral one.
//
// If SNOWFLAKE_TEST_CONN is unset, startSnowflake fails loudly so CI runs
// that select -db=snowflake produce an obvious "missing creds" error
// rather than silently skipping the suite.
func startSnowflake(ctx context.Context) (func(context.Context) error, string, error) {
	dsn := strings.TrimSpace(os.Getenv("SNOWFLAKE_TEST_CONN"))
	if dsn == "" {
		return nil, "", fmt.Errorf("SNOWFLAKE_TEST_CONN is not set — real Snowflake credentials are required for the snowflake test suite (format: USER:PASS@ACCOUNT/DATABASE?warehouse=WH&role=ROLE)")
	}

	schemaName, err := newEphemeralSchemaName()
	if err != nil {
		return nil, "", fmt.Errorf("snowflake: generating ephemeral schema name: %w", err)
	}

	// Rewrite the DSN to point at the ephemeral schema. We do this AFTER
	// CREATE SCHEMA so the seed ops and all subsequent test code open
	// sessions that default to the new schema.
	dsnWithSchema, err := applySchemaToSnowflakeDSN(dsn, schemaName)
	if err != nil {
		return nil, "", fmt.Errorf("snowflake: rewriting DSN schema: %w", err)
	}

	// Open an admin connection against the *original* DSN (i.e. whatever
	// schema it came with) to create the new schema.
	adminDB, err := sql.Open("snowflake", dsn)
	if err != nil {
		return nil, "", fmt.Errorf("snowflake: opening admin connection: %w", err)
	}
	if err := adminDB.PingContext(ctx); err != nil {
		_ = adminDB.Close()
		return nil, "", fmt.Errorf("snowflake: ping admin connection: %w (check SNOWFLAKE_TEST_CONN credentials, warehouse, and role)", err)
	}

	if _, err := adminDB.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS `+quoteSnowflakeIdent(schemaName)); err != nil {
		_ = adminDB.Close()
		return nil, "", fmt.Errorf("snowflake: CREATE SCHEMA %s: %w", schemaName, err)
	}

	// Seed the new schema. Use a fresh connection so USE SCHEMA has clean
	// session state and the script's CREATE TABLEs land in the right place.
	seedDB, err := sql.Open("snowflake", dsnWithSchema)
	if err != nil {
		_ = dropSnowflakeSchema(ctx, adminDB, schemaName)
		_ = adminDB.Close()
		return nil, "", fmt.Errorf("snowflake: opening seed connection: %w", err)
	}
	if err := seedDB.PingContext(ctx); err != nil {
		_ = seedDB.Close()
		_ = dropSnowflakeSchema(ctx, adminDB, schemaName)
		_ = adminDB.Close()
		return nil, "", fmt.Errorf("snowflake: ping seed connection: %w", err)
	}
	if _, err := seedDB.ExecContext(ctx, `USE SCHEMA `+quoteSnowflakeIdent(schemaName)); err != nil {
		_ = seedDB.Close()
		_ = dropSnowflakeSchema(ctx, adminDB, schemaName)
		_ = adminDB.Close()
		return nil, "", fmt.Errorf("snowflake: USE SCHEMA %s: %w", schemaName, err)
	}

	script, err := os.ReadFile("./snowflake.sql")
	if err != nil {
		_ = seedDB.Close()
		_ = dropSnowflakeSchema(ctx, adminDB, schemaName)
		_ = adminDB.Close()
		return nil, "", fmt.Errorf("snowflake: reading ./snowflake.sql: %w", err)
	}

	for _, stmt := range strings.Split(string(script), ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := seedDB.ExecContext(ctx, stmt); err != nil {
			_ = seedDB.Close()
			_ = dropSnowflakeSchema(ctx, adminDB, schemaName)
			_ = adminDB.Close()
			return nil, "", fmt.Errorf("snowflake: seeding (%s): %w\nSQL: %s", schemaName, err, stmt)
		}
	}
	_ = seedDB.Close()

	cleanup := func(ctx context.Context) error {
		defer adminDB.Close() //nolint:errcheck
		return dropSnowflakeSchema(ctx, adminDB, schemaName)
	}

	return cleanup, dsnWithSchema, nil
}

// newEphemeralSchemaName produces a stable-format random schema identifier
// that the test harness can CREATE/DROP without colliding with concurrent
// CI runs or user data. Uppercase matches Snowflake's default identifier
// storage so the quoted CREATE SCHEMA is idempotent with case-insensitive
// lookups later.
func newEphemeralSchemaName() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "GJ_TEST_" + strings.ToUpper(hex.EncodeToString(b[:])), nil
}

// quoteSnowflakeIdent double-quotes an identifier for safe use in DDL.
// Snowflake preserves the exact case of a double-quoted identifier.
func quoteSnowflakeIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// dropSnowflakeSchema tears down the ephemeral schema. Errors are returned
// so the caller can surface them, but CASCADE is specified so any tables
// created by the seed script (or by tests) are removed regardless.
func dropSnowflakeSchema(ctx context.Context, db *sql.DB, name string) error {
	_, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+quoteSnowflakeIdent(name)+` CASCADE`)
	if err != nil {
		return fmt.Errorf("DROP SCHEMA %s: %w", name, err)
	}
	return nil
}

// applySchemaToSnowflakeDSN replaces (or appends) the schema component in
// a gosnowflake DSN so subsequent connections land in the ephemeral schema
// we just created. Accepts both forms the driver recognizes:
//
//	user:pass@account/DB?params           (no schema)
//	user:pass@account/DB/SCHEMA?params    (existing schema — replaced)
func applySchemaToSnowflakeDSN(dsn, schema string) (string, error) {
	at := strings.IndexByte(dsn, '@')
	if at < 0 {
		return "", fmt.Errorf("missing '@' in DSN")
	}
	userInfo := dsn[:at]
	rest := dsn[at+1:]

	var query string
	if qIdx := strings.IndexByte(rest, '?'); qIdx >= 0 {
		query = rest[qIdx:]
		rest = rest[:qIdx]
	}

	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("DSN must include ACCOUNT/DATABASE; got %q", rest)
	}
	account := parts[0]
	database := parts[1]

	return userInfo + "@" + account + "/" + database + "/" + schema + query, nil
}
