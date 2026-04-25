package serv

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	core "github.com/dosco/graphjin/core/v3"
)

const rowCountQueryTimeout = 5 * time.Second

// approxRowCount fetches a single table's approximate row count from
// dialect-native catalog stats. Used by TableSample (Tier 3) for per-table
// drill-in. Tier 2 namespace-batched row counts use buildRowCountsForNamespace.
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
	case "oracle":
		return oracleRowCount(qctx, db, schema)
	case "mssql":
		return mssqlRowCount(qctx, db, schema)
	case "mongodb":
		return mongodbRowCount(qctx, db, schema)
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
	return 0, false
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
	var stat sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT stat FROM sqlite_stat1 WHERE tbl = ? LIMIT 1`,
		schema.Name,
	).Scan(&stat)
	if err != nil || !stat.Valid || stat.String == "" {
		return 0, false
	}
	fields := strings.Fields(stat.String)
	if len(fields) == 0 {
		return 0, false
	}
	var n int64
	if _, err := fmt.Sscanf(fields[0], "%d", &n); err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func oracleRowCount(ctx context.Context, db *sql.DB, schema *core.TableSchema) (int64, bool) {
	if schema.Schema == "" {
		return 0, false
	}
	var n sql.NullInt64
	err := db.QueryRowContext(ctx,
		`SELECT NUM_ROWS FROM ALL_TABLES WHERE OWNER = UPPER(:1) AND TABLE_NAME = UPPER(:2)`,
		schema.Schema, schema.Name,
	).Scan(&n)
	if err != nil && err != sql.ErrNoRows {
		log.Printf("discovery rowcount: oracle ALL_TABLES lookup failed for %s.%s: %v", schema.Schema, schema.Name, err)
		return 0, false
	}
	if !n.Valid || n.Int64 <= 0 {
		return 0, false
	}
	return n.Int64, true
}

func mssqlRowCount(ctx context.Context, db *sql.DB, schema *core.TableSchema) (int64, bool) {
	qual := mssqlBracketQuote(schema.Name)
	if schema.Schema != "" {
		qual = mssqlBracketQuote(schema.Schema) + "." + qual
	}
	var n sql.NullInt64
	err := db.QueryRowContext(ctx,
		`SELECT SUM(row_count) FROM sys.dm_db_partition_stats WHERE object_id = OBJECT_ID(@p1) AND index_id IN (0, 1)`,
		qual,
	).Scan(&n)
	if err != nil && err != sql.ErrNoRows {
		log.Printf("discovery rowcount: mssql sys.dm_db_partition_stats lookup failed for %s: %v", qual, err)
		return 0, false
	}
	if !n.Valid || n.Int64 <= 0 {
		return 0, false
	}
	return n.Int64, true
}

func mssqlBracketQuote(s string) string {
	return "[" + strings.ReplaceAll(s, "]", "]]") + "]"
}

func mongodbRowCount(_ context.Context, _ *sql.DB, _ *core.TableSchema) (int64, bool) {
	return 0, false
}

// buildRowCountsForNamespace fetches approximate row counts for every table
// in a single (database, schema) namespace via one catalog query per dialect.
// At Snowflake-scale (50 schemas × 100 tables) this turns 5000 round trips
// into 1 round trip per namespace the caller actually visits.
func buildRowCountsForNamespace(ctx context.Context, gj *core.GraphJin, database, schema string) (map[string]int64, error) {
	out := map[string]int64{}
	db, dbtype, err := gj.DBForDatabase(database)
	if err != nil {
		return out, err
	}
	if db == nil {
		return out, nil
	}

	qctx, cancel := context.WithTimeout(ctx, rowCountQueryTimeout)
	defer cancel()

	switch strings.ToLower(dbtype) {
	case "postgres", "postgresql", "cockroachdb", "cockroach":
		return postgresNamespaceRowCounts(qctx, db, schema)
	case "mysql", "mariadb":
		return mysqlNamespaceRowCounts(qctx, db, namespaceForSingleTier(database, schema))
	case "snowflake":
		return snowflakeNamespaceRowCounts(qctx, db, schema)
	case "sqlite":
		return sqliteNamespaceRowCounts(qctx, db)
	case "oracle":
		return oracleNamespaceRowCounts(qctx, db, schema)
	case "mssql":
		return mssqlNamespaceRowCounts(qctx, db, schema)
	case "mongodb":
		return out, nil
	}
	return out, nil
}

// namespaceForSingleTier returns the effective namespace identifier for
// dialects where database == schema (mysql, mariadb). When the caller
// supplies an empty schema we fall back to the database name.
func namespaceForSingleTier(database, schema string) string {
	if schema != "" {
		return schema
	}
	return database
}

func postgresNamespaceRowCounts(ctx context.Context, db *sql.DB, schema string) (map[string]int64, error) {
	if schema == "" {
		schema = "public"
	}
	const q = `
SELECT c.relname,
       GREATEST(c.reltuples, 0)::bigint
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1
  AND c.relkind IN ('r','p','m','f')`
	return scanRowCountPairs(ctx, db, q, schema)
}

func mysqlNamespaceRowCounts(ctx context.Context, db *sql.DB, schema string) (map[string]int64, error) {
	if schema == "" {
		return map[string]int64{}, nil
	}
	const q = `
SELECT TABLE_NAME, COALESCE(TABLE_ROWS, 0)
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE'`
	return scanRowCountPairs(ctx, db, q, schema)
}

func snowflakeNamespaceRowCounts(ctx context.Context, db *sql.DB, schema string) (map[string]int64, error) {
	if schema == "" {
		return map[string]int64{}, nil
	}
	const q = `
SELECT TABLE_NAME, COALESCE(ROW_COUNT, 0)
FROM INFORMATION_SCHEMA.TABLES
WHERE UPPER(TABLE_SCHEMA) = UPPER(?) AND TABLE_TYPE = 'BASE TABLE'`
	out, err := scanRowCountPairs(ctx, db, q, schema)
	return lowercaseKeys(out), err
}

func sqliteNamespaceRowCounts(ctx context.Context, db *sql.DB) (map[string]int64, error) {
	const q = `
SELECT m.name,
       COALESCE(CAST(substr(s.stat,1,instr(s.stat||' ',' ')-1) AS INTEGER), 0)
FROM sqlite_master m
LEFT JOIN sqlite_stat1 s ON s.tbl = m.name
WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%'`
	return scanRowCountPairs(ctx, db, q)
}

func oracleNamespaceRowCounts(ctx context.Context, db *sql.DB, schema string) (map[string]int64, error) {
	if schema == "" {
		return map[string]int64{}, nil
	}
	const q = `
SELECT TABLE_NAME, COALESCE(NUM_ROWS, 0)
FROM ALL_TABLES
WHERE OWNER = UPPER(:1)`
	out, err := scanRowCountPairs(ctx, db, q, schema)
	return lowercaseKeys(out), err
}

func mssqlNamespaceRowCounts(ctx context.Context, db *sql.DB, schema string) (map[string]int64, error) {
	if schema == "" {
		schema = "dbo"
	}
	const q = `
SELECT t.name, COALESCE(SUM(CAST(p.row_count AS BIGINT)), 0)
FROM sys.tables t
JOIN sys.schemas s ON s.schema_id = t.schema_id
LEFT JOIN sys.dm_db_partition_stats p
       ON p.object_id = t.object_id AND p.index_id IN (0, 1)
WHERE s.name = @p1
GROUP BY t.name`
	return scanRowCountPairs(ctx, db, q, schema)
}

// lowercaseKeys normalizes a row-count map's keys to lowercase. Oracle and
// Snowflake store unquoted identifiers as uppercase in their catalog views;
// core/internal/sdata lowercases on load, so we must match.
func lowercaseKeys(in map[string]int64) map[string]int64 {
	if len(in) == 0 {
		return in
	}
	out := make(map[string]int64, len(in))
	for k, v := range in {
		out[strings.ToLower(k)] = v
	}
	return out
}

func scanRowCountPairs(ctx context.Context, db *sql.DB, q string, args ...any) (map[string]int64, error) {
	out := map[string]int64{}
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			name sql.NullString
			n    sql.NullInt64
		)
		if err := rows.Scan(&name, &n); err != nil {
			return out, err
		}
		if name.Valid {
			out[name.String] = n.Int64
		}
	}
	return out, rows.Err()
}
