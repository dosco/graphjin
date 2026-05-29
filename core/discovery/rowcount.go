package discovery

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	core "github.com/dosco/graphjin/core/v3"
)

const QueryTimeout = 5 * time.Second

// ApproxRowCount returns a single-table reltuples-equivalent lookup.
func ApproxRowCount(ctx context.Context, db *sql.DB, dbtype string, schema *core.TableSchema) (int64, bool) {
	qctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()

	switch strings.ToLower(dbtype) {
	case "postgres", "postgresql", "cockroachdb", "cockroach":
		return postgresRowCount(qctx, db, schema)
	case "mysql", "mariadb":
		return mysqlRowCount(qctx, db, schema)
	case "snowflake":
		return snowflakeRowCount(qctx, db, schema)
	case "bigquery":
		return bigqueryRowCount(qctx, db, schema)
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

func snowflakeRowCount(_ context.Context, _ *sql.DB, _ *core.TableSchema) (int64, bool) {
	// INFORMATION_SCHEMA is restricted on some Snowflake accounts, so discovery
	// must never depend on it. Approximate row counts are unavailable via this
	// path; SHOW TABLES exposes a "rows" column but needs a pinned RESULT_SCAN
	// session, so it is left as a future enhancement.
	return 0, false
}

func bigqueryRowCount(ctx context.Context, db *sql.DB, schema *core.TableSchema) (int64, bool) {
	var n sql.NullInt64
	q := `SELECT TOTAL_ROWS FROM INFORMATION_SCHEMA.TABLE_STORAGE WHERE LOWER(TABLE_NAME) = LOWER(?) AND TABLE_TYPE = 'BASE TABLE'`
	args := []any{schema.Name}
	if schema.Schema != "" {
		q = `SELECT TOTAL_ROWS FROM INFORMATION_SCHEMA.TABLE_STORAGE WHERE LOWER(TABLE_SCHEMA) = LOWER(?) AND LOWER(TABLE_NAME) = LOWER(?) AND TABLE_TYPE = 'BASE TABLE'`
		args = []any{schema.Schema, schema.Name}
	}
	if err := db.QueryRowContext(ctx, q, args...).Scan(&n); err == nil && n.Valid && n.Int64 >= 0 {
		return n.Int64, true
	} else if err != nil {
		log.Printf("discovery rowcount: bigquery information_schema lookup failed for %s.%s: %v", schema.Schema, schema.Name, err)
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

// RowCountsForNamespace returns counts for every table in one namespace with one catalog query.
func RowCountsForNamespace(ctx context.Context, gj *core.GraphJin, database, schema string) (map[string]int64, error) {
	out := map[string]int64{}
	db, dbtype, err := gj.DBForDatabase(database)
	if err != nil {
		return out, err
	}
	if db == nil {
		return out, nil
	}

	qctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()

	switch strings.ToLower(dbtype) {
	case "postgres", "postgresql", "cockroachdb", "cockroach":
		return postgresNamespaceRowCounts(qctx, db, schema)
	case "mysql", "mariadb":
		return mysqlNamespaceRowCounts(qctx, db, NamespaceForSingleTier(database, schema))
	case "snowflake":
		return snowflakeNamespaceRowCounts(qctx, db, schema)
	case "bigquery":
		return bigqueryNamespaceRowCounts(qctx, db, schema)
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

// NamespaceForSingleTier returns the namespace for mysql/mariadb where database == schema.
func NamespaceForSingleTier(database, schema string) string {
	if schema != "" {
		return schema
	}
	return database
}

func postgresNamespaceRowCounts(ctx context.Context, db *sql.DB, schema string) (map[string]int64, error) {
	if schema == "" {
		schema = "public"
	}
	// reltuples = -1 sentinel ("never analyzed") → NULL → skipped.
	const q = `
SELECT c.relname,
       CASE WHEN c.reltuples >= 0 THEN c.reltuples::bigint END
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
	// NULL TABLE_ROWS → unknown (no COALESCE, omitempty preserves the signal).
	const q = `
SELECT TABLE_NAME, TABLE_ROWS
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE'`
	return scanRowCountPairs(ctx, db, q, schema)
}

func snowflakeNamespaceRowCounts(_ context.Context, _ *sql.DB, _ string) (map[string]int64, error) {
	// INFORMATION_SCHEMA is restricted on some Snowflake accounts; skip it so
	// discovery never depends on it. Row counts are unavailable via this path.
	return map[string]int64{}, nil
}

func bigqueryNamespaceRowCounts(ctx context.Context, db *sql.DB, schema string) (map[string]int64, error) {
	q := `
SELECT TABLE_NAME, TOTAL_ROWS
FROM INFORMATION_SCHEMA.TABLE_STORAGE
WHERE TABLE_TYPE = 'BASE TABLE'
  AND (
    COALESCE(@@dataset_id, '') = ''
    OR LOWER(TABLE_SCHEMA) = LOWER(COALESCE(@@dataset_id, ''))
  )`
	var args []any
	if schema != "" {
		q += ` AND LOWER(TABLE_SCHEMA) = LOWER(?)`
		args = append(args, schema)
	}
	out, err := scanRowCountPairs(ctx, db, q, args...)
	return lowercaseKeys(out), err
}

func sqliteNamespaceRowCounts(ctx context.Context, db *sql.DB) (map[string]int64, error) {
	// LEFT JOIN: tables without sqlite_stat1 entries get NULL → skipped.
	const q = `
SELECT m.name,
       CAST(substr(s.stat,1,instr(s.stat||' ',' ')-1) AS INTEGER)
FROM sqlite_master m
LEFT JOIN sqlite_stat1 s ON s.tbl = m.name
WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%'`
	return scanRowCountPairs(ctx, db, q)
}

func oracleNamespaceRowCounts(ctx context.Context, db *sql.DB, schema string) (map[string]int64, error) {
	if schema == "" {
		return map[string]int64{}, nil
	}
	// NULL NUM_ROWS → never analyzed → unknown.
	const q = `
SELECT TABLE_NAME, NUM_ROWS
FROM ALL_TABLES
WHERE OWNER = UPPER(:1)`
	out, err := scanRowCountPairs(ctx, db, q, schema)
	return lowercaseKeys(out), err
}

func mssqlNamespaceRowCounts(ctx context.Context, db *sql.DB, schema string) (map[string]int64, error) {
	if schema == "" {
		schema = "dbo"
	}
	// LEFT JOIN miss → SUM returns NULL → unknown.
	const q = `
SELECT t.name, SUM(CAST(p.row_count AS BIGINT))
FROM sys.tables t
JOIN sys.schemas s ON s.schema_id = t.schema_id
LEFT JOIN sys.dm_db_partition_stats p
       ON p.object_id = t.object_id AND p.index_id IN (0, 1)
WHERE s.name = @p1
GROUP BY t.name`
	return scanRowCountPairs(ctx, db, q, schema)
}

// lowercaseKeys: oracle/snowflake catalog uppercases; core lowercases on load.
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
		// NULL count = unknown; skip so callers don't confuse with measured zero.
		if name.Valid && n.Valid {
			out[name.String] = n.Int64
		}
	}
	return out, rows.Err()
}
