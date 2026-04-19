package serv

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"time"

	core "github.com/dosco/graphjin/core/v3"
)

const rowCountQueryTimeout = 5 * time.Second

func buildRowCounts(ctx context.Context, gj *core.GraphJin, database string, schemas []*core.TableSchema) map[string]int64 {
	out := make(map[string]int64, len(schemas))
	db, dbtype, err := gj.DBForDatabase(database)
	if err != nil || db == nil {
		return out
	}
	for _, s := range schemas {
		if n, ok := approxRowCount(ctx, db, dbtype, s); ok {
			out[s.Name] = n
		}
	}
	return out
}

func approxRowCount(ctx context.Context, db *sql.DB, dbtype string, schema *core.TableSchema) (int64, bool) {
	qctx, cancel := context.WithTimeout(ctx, rowCountQueryTimeout)
	defer cancel()

	switch strings.ToLower(dbtype) {
	case "postgres", "postgresql", "cockroachdb", "cockroach":
		return postgresRowCount(qctx, db, schema)
	case "mysql", "mariadb":
		return mysqlRowCount(qctx, db, schema)
	case "snowflake":
		return snowflakeRowCount(qctx, db, schema)
	case "sqlite":
		return sqliteRowCount(qctx, db, schema)
	}
	return 0, false
}

func postgresRowCount(ctx context.Context, db *sql.DB, schema *core.TableSchema) (int64, bool) {
	qual := schema.Name
	if schema.Schema != "" {
		qual = schema.Schema + "." + qual
	}
	var relkind sql.NullString
	var reltuples sql.NullFloat64
	err := db.QueryRowContext(ctx,
		`SELECT relkind, reltuples FROM pg_class WHERE oid = to_regclass($1)`, qual,
	).Scan(&relkind, &reltuples)
	if err == sql.ErrNoRows || !relkind.Valid {
		return 0, false
	}
	if err != nil {
		log.Printf("discovery rowcount: pg_class lookup failed for %s: %v", qual, err)
		return 0, false
	}
	if relkind.String != "r" && relkind.String != "p" && relkind.String != "m" && relkind.String != "f" {
		return 0, false
	}
	if reltuples.Valid && reltuples.Float64 > 0 {
		return int64(reltuples.Float64), true
	}
	quoted := `"` + strings.ReplaceAll(schema.Name, `"`, `""`) + `"`
	if schema.Schema != "" {
		quoted = `"` + strings.ReplaceAll(schema.Schema, `"`, `""`) + `".` + quoted
	}
	var count int64
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoted).Scan(&count); err != nil {
		log.Printf("discovery rowcount: count(*) failed for %s: %v", quoted, err)
		return 0, false
	}
	return count, true
}

func mysqlRowCount(ctx context.Context, db *sql.DB, schema *core.TableSchema) (int64, bool) {
	var n sql.NullInt64
	q := `SELECT TABLE_ROWS FROM information_schema.tables WHERE TABLE_NAME = ?`
	args := []any{schema.Name}
	if schema.Schema != "" {
		q = `SELECT TABLE_ROWS FROM information_schema.tables WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`
		args = []any{schema.Schema, schema.Name}
	}
	if err := db.QueryRowContext(ctx, q, args...).Scan(&n); err == nil && n.Valid && n.Int64 >= 0 {
		return n.Int64, true
	} else if err != nil {
		log.Printf("discovery rowcount: mysql information_schema lookup failed for %s.%s: %v", schema.Schema, schema.Name, err)
	}
	return 0, false
}

func snowflakeRowCount(ctx context.Context, db *sql.DB, schema *core.TableSchema) (int64, bool) {
	var n sql.NullInt64
	q := `SELECT ROW_COUNT FROM INFORMATION_SCHEMA.TABLES WHERE UPPER(TABLE_NAME) = UPPER(?)`
	args := []any{schema.Name}
	if schema.Schema != "" {
		q = `SELECT ROW_COUNT FROM INFORMATION_SCHEMA.TABLES WHERE UPPER(TABLE_SCHEMA) = UPPER(?) AND UPPER(TABLE_NAME) = UPPER(?)`
		args = []any{schema.Schema, schema.Name}
	}
	if err := db.QueryRowContext(ctx, q, args...).Scan(&n); err == nil && n.Valid && n.Int64 >= 0 {
		return n.Int64, true
	} else if err != nil {
		log.Printf("discovery rowcount: snowflake information_schema lookup failed for %s.%s: %v", schema.Schema, schema.Name, err)
	}
	return 0, false
}

func sqliteRowCount(ctx context.Context, db *sql.DB, schema *core.TableSchema) (int64, bool) {
	quoted := `"` + strings.ReplaceAll(schema.Name, `"`, `""`) + `"`
	var n int64
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoted).Scan(&n); err != nil {
		log.Printf("discovery rowcount: sqlite count(*) failed for %s: %v", quoted, err)
		return 0, false
	}
	return n, true
}
