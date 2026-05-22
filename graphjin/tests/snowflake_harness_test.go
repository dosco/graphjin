package tests_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/dosco/graphjin/tests/v3/snowflakeemu"
	"github.com/snowflakedb/gosnowflake"
)

const snowflakeConnEnv = "SNOWFLAKE_TEST_CONN"

func init() {
	_ = gosnowflake.GetLogger().SetLogLevel("fatal")
}

func startSnowflakeDB(ctx context.Context) (func(context.Context) error, *sql.DB, error) {
	if os.Getenv("GRAPHJIN_SNOWFLAKE_MOCK") == "1" {
		sqlDB := sql.OpenDB(snowflakeemu.NewConnector(snowflakeemu.Config{
			SeedPath:   "./snowflake.sql",
			CaptureDir: strings.TrimSpace(os.Getenv("GRAPHJIN_SNOWFLAKE_CAPTURE_DIR")),
			TestName:   strings.TrimSpace(os.Getenv("GRAPHJIN_SNOWFLAKE_TEST_NAME")),
			RunID:      strings.TrimSpace(os.Getenv("GRAPHJIN_SNOWFLAKE_RUN_ID")),
			Backend:    strings.TrimSpace(os.Getenv("GRAPHJIN_SNOWFLAKE_BACKEND")),
			Fallback:   strings.TrimSpace(os.Getenv("GRAPHJIN_SNOWFLAKE_FALLBACK")),
			Discovery:  strings.TrimSpace(os.Getenv("GRAPHJIN_SNOWFLAKE_DISCOVERY")),
		}))
		cleanup := func(context.Context) error {
			return sqlDB.Close()
		}
		if err := sqlDB.PingContext(ctx); err != nil {
			_ = cleanup(ctx)
			return nil, nil, err
		}
		return cleanup, sqlDB, nil
	}

	cleanup, dsn, err := startSnowflake(ctx)
	if err != nil {
		return nil, nil, err
	}
	sqlDB, err := sql.Open("snowflake", dsn)
	if err != nil {
		_ = cleanup(ctx)
		return nil, nil, err
	}
	return func(ctx context.Context) error {
		_ = sqlDB.Close()
		return cleanup(ctx)
	}, sqlDB, nil
}

func startSnowflake(ctx context.Context) (func(context.Context) error, string, error) {
	dsn := strings.TrimSpace(os.Getenv(snowflakeConnEnv))
	if dsn == "" {
		log.Printf("Skipping snowflake tests: %s env var not set", snowflakeConnEnv)
		os.Exit(0)
	}

	schemaName, err := newEphemeralSchemaName()
	if err != nil {
		return nil, "", fmt.Errorf("snowflake: generating ephemeral schema name: %w", err)
	}

	dsnWithSchema, err := applySchemaToSnowflakeDSN(dsn, schemaName)
	if err != nil {
		return nil, "", fmt.Errorf("snowflake: rewriting DSN schema: %w", err)
	}

	adminDB, err := sql.Open("snowflake", dsn)
	if err != nil {
		return nil, "", fmt.Errorf("snowflake: opening admin connection: %w", err)
	}
	if err := adminDB.PingContext(ctx); err != nil {
		_ = adminDB.Close()
		return nil, "", fmt.Errorf("snowflake: ping admin connection: %w (check %s credentials, warehouse, and role)", err, snowflakeConnEnv)
	}

	if _, err := adminDB.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS `+quoteSnowflakeIdent(schemaName)); err != nil {
		_ = adminDB.Close()
		return nil, "", fmt.Errorf("snowflake: CREATE SCHEMA %s: %w", schemaName, err)
	}

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

func newEphemeralSchemaName() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "GJ_TEST_" + strings.ToUpper(hex.EncodeToString(b[:])), nil
}

func quoteSnowflakeIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func dropSnowflakeSchema(ctx context.Context, db *sql.DB, name string) error {
	_, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+quoteSnowflakeIdent(name)+` CASCADE`)
	if err != nil {
		return fmt.Errorf("DROP SCHEMA %s: %w", name, err)
	}
	return nil
}

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
