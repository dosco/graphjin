package discovery

import (
	"context"
	"database/sql"
	"log"
	"sort"
	"strings"

	core "github.com/dosco/graphjin/core/v3"
)

// Namespaces returns one catalog rollup per (database, schema).
func Namespaces(ctx context.Context, gj *core.GraphJin, database string) ([]NamespaceRollup, error) {
	db, dbtype, err := gj.DBForDatabase(database)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return nil, nil
	}

	qctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()

	var rows []NamespaceRollup
	switch strings.ToLower(dbtype) {
	case "postgres", "postgresql", "cockroachdb", "cockroach":
		rows, err = postgresNamespaceRollup(qctx, db)
	case "mysql", "mariadb":
		rows, err = mysqlNamespaceRollup(qctx, db)
	case "snowflake":
		rows, err = snowflakeNamespaceRollup(qctx, db, loadedSchemas(gj, database))
	case "bigquery":
		rows, err = bigqueryNamespaceRollup(qctx, db)
	case "sqlite":
		rows, err = sqliteNamespaceRollup(qctx, db)
	case "oracle":
		rows, err = oracleNamespaceRollup(qctx, db)
	case "mssql":
		rows, err = mssqlNamespaceRollup(qctx, db)
	case "mongodb":
		rows = mongodbNamespaceRollup(gj, database)
	default:
		rows = fallbackNamespaceRollup(gj, database)
	}
	if err != nil {
		log.Printf("discovery namespace rollup: %s: %v", dbtype, err)
		return fallbackNamespaceRollup(gj, database), nil
	}
	if len(rows) == 0 {
		return fallbackNamespaceRollup(gj, database), nil
	}
	sortNamespaceRollup(rows)
	return rows, nil
}

func loadedSchemas(gj *core.GraphJin, database string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range gj.GetTablesForDatabase(database) {
		schema := strings.TrimSpace(t.Schema)
		if schema == "" || seen[strings.ToLower(schema)] {
			continue
		}
		seen[strings.ToLower(schema)] = true
		out = append(out, schema)
	}
	sort.Strings(out)
	return out
}

func sortNamespaceRollup(rows []NamespaceRollup) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Database != rows[j].Database {
			return rows[i].Database < rows[j].Database
		}
		return rows[i].Schema < rows[j].Schema
	})
}

func postgresNamespaceRollup(ctx context.Context, db *sql.DB) ([]NamespaceRollup, error) {
	const q = `
SELECT current_database() AS database,
       n.nspname AS schema,
       COUNT(*) FILTER (WHERE c.relkind IN ('r','p','m','f')) AS table_count,
       COALESCE(SUM(GREATEST(c.reltuples, 0)::bigint)
                FILTER (WHERE c.relkind IN ('r','p','m','f')), 0) AS approx_row_total
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname NOT IN ('pg_catalog','information_schema','pg_toast')
  AND n.nspname NOT LIKE 'pg_temp_%'
  AND n.nspname NOT LIKE 'pg_toast_temp_%'
GROUP BY n.nspname
HAVING COUNT(*) FILTER (WHERE c.relkind IN ('r','p','m','f')) > 0`
	return scanNamespaceRollup(ctx, db, q, true)
}

func mysqlNamespaceRollup(ctx context.Context, db *sql.DB) ([]NamespaceRollup, error) {
	const q = `
SELECT TABLE_SCHEMA AS db, TABLE_SCHEMA AS sch,
       COUNT(*) AS table_count,
       COALESCE(SUM(TABLE_ROWS), 0) AS approx_row_total
FROM information_schema.TABLES
WHERE TABLE_TYPE = 'BASE TABLE'
  AND TABLE_SCHEMA NOT IN ('mysql','sys','performance_schema','information_schema')
GROUP BY TABLE_SCHEMA`
	rows, err := scanNamespaceRollup(ctx, db, q, true)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].Schema = ""
	}
	return rows, nil
}

func snowflakeNamespaceRollup(ctx context.Context, db *sql.DB, schemas []string) ([]NamespaceRollup, error) {
	if len(schemas) == 0 {
		return nil, nil
	}
	// Per-schema table counts and approximate row totals from SHOW TABLES read via
	// RESULT_SCAN (no INFORMATION_SCHEMA). SHOW + RESULT_SCAN share one connection.
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var dbName string
	_ = conn.QueryRowContext(ctx, "SELECT COALESCE(CURRENT_DATABASE(), '')").Scan(&dbName)

	if _, err := conn.ExecContext(ctx, "SHOW TABLES"); err != nil {
		return nil, err
	}
	var qid string
	if err := conn.QueryRowContext(ctx, "SELECT LAST_QUERY_ID()").Scan(&qid); err != nil {
		return nil, err
	}

	const q = `SELECT LOWER("schema_name") AS schema_name,
       COUNT(*) AS table_count,
       COALESCE(SUM("rows"), 0) AS approx_row_total
FROM TABLE(RESULT_SCAN(?))
GROUP BY LOWER("schema_name")`
	rows, err := conn.QueryContext(ctx, q, qid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []NamespaceRollup
	for rows.Next() {
		var sch sql.NullString
		var tableCount, rowTotal sql.NullInt64
		if err := rows.Scan(&sch, &tableCount, &rowTotal); err != nil {
			return nil, err
		}
		out = append(out, NamespaceRollup{
			Database:          dbName,
			Schema:            sch.String,
			TableCount:        int(tableCount.Int64),
			ApproxRowTotal:    rowTotal.Int64,
			RowCountAvailable: true,
		})
	}
	return out, rows.Err()
}

func bigqueryNamespaceRollup(ctx context.Context, db *sql.DB) ([]NamespaceRollup, error) {
	const q = `
SELECT COALESCE(@@project_id, '') AS db,
       TABLE_SCHEMA AS sch,
       COUNT(*) AS table_count,
       COALESCE(SUM(TOTAL_ROWS), 0) AS approx_row_total
FROM INFORMATION_SCHEMA.TABLE_STORAGE
WHERE TABLE_TYPE = 'BASE TABLE'
  AND (
    COALESCE(@@dataset_id, '') = ''
    OR LOWER(TABLE_SCHEMA) = LOWER(COALESCE(@@dataset_id, ''))
  )
GROUP BY TABLE_SCHEMA`
	rows, err := scanNamespaceRollup(ctx, db, q, true)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].Schema = strings.ToLower(rows[i].Schema)
	}
	return rows, nil
}

func sqliteNamespaceRollup(ctx context.Context, db *sql.DB) ([]NamespaceRollup, error) {
	const q = `
SELECT 'main' AS db, '' AS sch,
       (SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%') AS table_count,
       COALESCE((SELECT SUM(CAST(substr(stat,1,instr(stat||' ',' ')-1) AS INTEGER)) FROM sqlite_stat1), 0) AS approx_row_total`
	return scanNamespaceRollup(ctx, db, q, true)
}

func oracleNamespaceRollup(ctx context.Context, db *sql.DB) ([]NamespaceRollup, error) {
	const q = `
SELECT SYS_CONTEXT('USERENV','DB_NAME') AS db,
       OWNER,
       COUNT(*) AS table_count,
       COALESCE(SUM(NUM_ROWS), 0) AS approx_row_total
FROM ALL_TABLES
WHERE OWNER NOT IN ('SYS','SYSTEM','XDB','MDSYS','CTXSYS','OUTLN','ORDSYS',
                    'GSMADMIN_INTERNAL','APPQOSSYS','DBSNMP','OJVMSYS','LBACSYS')
GROUP BY OWNER`
	return scanNamespaceRollup(ctx, db, q, true)
}

func mssqlNamespaceRollup(ctx context.Context, db *sql.DB) ([]NamespaceRollup, error) {
	const q = `
SELECT DB_NAME() AS db,
       s.name AS sch,
       COUNT(DISTINCT t.object_id) AS table_count,
       COALESCE(SUM(CAST(p.row_count AS BIGINT)), 0) AS approx_row_total
FROM sys.tables t
JOIN sys.schemas s ON s.schema_id = t.schema_id
LEFT JOIN sys.dm_db_partition_stats p
       ON p.object_id = t.object_id AND p.index_id IN (0, 1)
GROUP BY s.name`
	return scanNamespaceRollup(ctx, db, q, true)
}

// mongodbNamespaceRollup: synthesized; row counts unavailable via SQL.
func mongodbNamespaceRollup(gj *core.GraphJin, database string) []NamespaceRollup {
	tables := gj.GetTablesForDatabase(database)
	if len(tables) == 0 {
		return nil
	}
	return []NamespaceRollup{{
		Database:          database,
		TableCount:        len(tables),
		ApproxRowTotal:    0,
		RowCountAvailable: false,
	}}
}

// fallbackNamespaceRollup: in-memory synth when catalog query fails.
func fallbackNamespaceRollup(gj *core.GraphJin, database string) []NamespaceRollup {
	tables := gj.GetTablesForDatabase(database)
	if len(tables) == 0 {
		return nil
	}
	bySchema := map[string]*NamespaceRollup{}
	for _, t := range tables {
		key := t.Schema
		nr, ok := bySchema[key]
		if !ok {
			nr = &NamespaceRollup{
				Database: database,
				Schema:   t.Schema,
			}
			bySchema[key] = nr
		}
		nr.TableCount++
	}
	out := make([]NamespaceRollup, 0, len(bySchema))
	for _, nr := range bySchema {
		out = append(out, *nr)
	}
	sortNamespaceRollup(out)
	return out
}

// scanNamespaceRollup: 4-column scan (db, schema, table_count, row_total).
func scanNamespaceRollup(ctx context.Context, db *sql.DB, q string, rowCountsAvailable bool, args ...any) ([]NamespaceRollup, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []NamespaceRollup
	for rows.Next() {
		var (
			dbName     sql.NullString
			schemaName sql.NullString
			tableCount sql.NullInt64
			rowTotal   sql.NullInt64
		)
		if err := rows.Scan(&dbName, &schemaName, &tableCount, &rowTotal); err != nil {
			return nil, err
		}
		out = append(out, NamespaceRollup{
			Database:          dbName.String,
			Schema:            schemaName.String,
			TableCount:        int(tableCount.Int64),
			ApproxRowTotal:    rowTotal.Int64,
			RowCountAvailable: rowCountsAvailable,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
