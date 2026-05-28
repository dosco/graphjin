package introspection

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/internal/util"
	"golang.org/x/sync/errgroup"
)

type DBColumn = sdata.DBColumn
type DBFunction = sdata.DBFunction
type DBFuncParam = sdata.DBFuncParam
type DBInfo = sdata.DBInfo
type DBTable = sdata.DBTable
type CompositeFKInfo = sdata.CompositeFKInfo

// introspectionQueryTimeout bounds each individual schema-discovery SQL
// query. Without this, a hung network read from the driver (seen with
// go-ora against Oracle) could block a test run indefinitely.
const introspectionQueryTimeout = 30 * time.Second

var (
	errSnowflakeNoUserSchemas = errors.New("snowflake: no user schemas found")
	errSnowflakeNoColumns     = errors.New("snowflake: no columns found")
)

// GetDBInfo returns the database schema information.
//
// The context bounds the full discovery run (all queries + retries). Callers
// that don't have a context can use context.Background(); an internal
// per-query timeout (introspectionQueryTimeout) still applies on top so a
// hung driver read can't block forever.
func GetDBInfo(
	ctx context.Context,
	db *sql.DB,
	dbType string,
	blockList []string,
) (*DBInfo, error) {
	retryDelays := []time.Duration{
		0,
		50 * time.Millisecond,
		100 * time.Millisecond,
		250 * time.Millisecond,
		500 * time.Millisecond,
	}

	if ctx == nil {
		ctx = context.Background()
	}

	var lastErr error
	for i, delay := range retryDelays {
		if i > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		di, err := getDBInfoOnce(ctx, db, dbType, blockList)
		if err == nil {
			return di, nil
		}
		if !isRetryableDiscoveryError(err) {
			return nil, err
		}
		lastErr = err
	}

	return nil, lastErr
}

func getDBInfoOnce(
	ctx context.Context,
	db *sql.DB,
	dbType string,
	blockList []string,
) (*DBInfo, error) {
	var dbVersion int
	var dbSchema, dbName string
	var cols []DBColumn
	var funcs []DBFunction
	var compositeFKs []CompositeFKInfo
	var snowflakeClustering map[string][]string

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		qctx, cancel := context.WithTimeout(gctx, introspectionQueryTimeout)
		defer cancel()

		var row *sql.Row

		switch dbType {
		case "postgres", "":
			row = db.QueryRowContext(qctx, postgresInfo)
		case "mysql":
			row = db.QueryRowContext(qctx, mysqlInfo)
		case "mariadb":
			row = db.QueryRowContext(qctx, mariadbInfo)
		case "sqlite":
			row = db.QueryRowContext(qctx, sqliteInfo)
		case "oracle":
			row = db.QueryRowContext(qctx, oracleInfo)
		case "mssql":
			row = db.QueryRowContext(qctx, mssqlInfo)
		case "snowflake":
			row = db.QueryRowContext(qctx, snowflakeInfo)
		case "bigquery":
			row = db.QueryRowContext(qctx, bigqueryInfo)
		case "mongodb":
			// MongoDB returns info via the driver's introspection
			row = db.QueryRowContext(qctx, mongodbInfo)
		default:
			return fmt.Errorf("unsupported database type %q: supported types are postgres, mysql, mariadb, sqlite, oracle, mssql, snowflake, bigquery, mongodb", dbType)
		}

		if err := row.Scan(&dbVersion, &dbSchema, &dbName); err != nil {
			return err
		}
		if dbType == "oracle" {
			dbSchema = strings.ToLower(dbSchema)
		}
		return nil
	})

	if dbType == "snowflake" && !snowflakeSkipClusteringDiscovery() {
		g.Go(func() error {
			ck, err := discoverClusteringKeys(gctx, db)
			if err == nil {
				snowflakeClustering = ck
			}
			return nil
		})
	}

	g.Go(func() error {
		var err error
		if cols, err = DiscoverColumns(gctx, db, dbType, blockList); err != nil {
			return err
		}

		if funcs, err = DiscoverFunctions(gctx, db, dbType, blockList); err != nil {
			return err
		}

		// Short-circuit: if column-level FK data shows no table with
		// 2+ columns pointing at the same foreign table, there cannot
		// be any composite FKs and we can skip the expensive dialect-
		// specific introspection query. This is the biggest single
		// cost removal for the common case where test fixtures and
		// small schemas have at most one composite FK.
		if !hasCompositeFKCandidates(cols) {
			compositeFKs = nil
			return nil
		}

		if compositeFKs, err = DiscoverCompositeFKs(gctx, db, dbType); err != nil {
			return err
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	// When the database returns an empty default schema (e.g., PostgreSQL with
	// search_path=''), infer it from discovered columns by picking the schema
	// with the most tables. This ensures Find() lookups and cross-schema joins
	// work correctly even with non-standard search_path configurations.
	if dbSchema == "" && len(cols) > 0 {
		dbSchema = inferDefaultSchema(cols)
	}

	di := sdata.NewDBInfo(
		dbType,
		dbVersion,
		dbSchema,
		dbName,
		cols,
		funcs,
		blockList)
	di.CompositeFKs = compositeFKs

	// For Snowflake, discover clustering keys and attach to tables.
	// Non-fatal: if this fails we just skip clustering optimization.
	if dbType == "snowflake" && len(snowflakeClustering) != 0 {
		for i := range di.Tables {
			key := di.Tables[i].Schema + ":" + di.Tables[i].Name
			if keys, ok := snowflakeClustering[key]; ok {
				di.Tables[i].ClusteringKeys = keys

				// Auto-set partition key from leading clustering column if
				// it's a temporal type and no explicit partition config exists.
				// This enables automatic "missing partition filter" warnings.
				if di.Tables[i].PartitionKey == "" {
					autoSetPartitionFromClustering(&di.Tables[i])
				}
			}
		}
	}

	return di, nil
}

// hasCompositeFKCandidates returns true if any table in the column set has
// two or more columns referencing the same foreign (schema, table). This is
// a necessary condition for a composite foreign key to exist, derived
// entirely from data already collected by DiscoverColumns. If it returns
// false, the expensive DiscoverCompositeFKs query can be skipped.
func hasCompositeFKCandidates(cols []DBColumn) bool {
	counts := make(map[string]int)
	for i := range cols {
		c := &cols[i]
		if c.FKeyTable == "" {
			continue
		}
		k := c.Schema + ":" + c.Table + ":" + c.FKeySchema + ":" + c.FKeyTable
		counts[k]++
		if counts[k] > 1 {
			return true
		}
	}
	return false
}

func isRetryableDiscoveryError(err error) bool {
	if err == nil {
		return false
	}
	// Context cancellation / deadline errors are terminal — retrying will
	// almost always hit the same timeout and just multiply the wait.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, driver.ErrBadConn) {
		return true
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, " eof") ||
		strings.HasSuffix(msg, "eof") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "bad connection") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "broken pipe")
}

// DiscoverColumns returns the columns of a table
func DiscoverColumns(ctx context.Context, db *sql.DB, dbtype string, blockList []string) ([]DBColumn, error) {
	var sqlStmt string

	switch dbtype {
	case "postgres", "":
		return discoverPostgresColumns(ctx, db, blockList)
	case "mysql":
		return discoverMySQLColumns(ctx, db, "mysql", blockList)
	case "mariadb":
		return discoverMySQLColumns(ctx, db, "mariadb", blockList)
	case "sqlite":
		sqlStmt = sqliteColumnsStmt
	case "oracle":
		sqlStmt = oracleColumnsStmt
	case "mssql":
		sqlStmt = mssqlColumnsStmt
	case "snowflake":
		return discoverSnowflakeColumns(ctx, db, blockList)
	case "bigquery":
		return discoverBigQueryColumns(ctx, db, blockList)
	case "mongodb":
		// MongoDB uses JSON query DSL - the driver handles introspection
		sqlStmt = mongodbColumnsStmt
	default:
		return nil, fmt.Errorf("unsupported database type %q: supported types are postgres, mysql, mariadb, sqlite, oracle, mssql, snowflake, bigquery, mongodb", dbtype)
	}

	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	start := time.Now()
	rows, err := db.QueryContext(qctx, sqlStmt)
	recordDiscoveryQuery(ctx, "schema_metadata", sqlStmt, time.Since(start))
	if err != nil {
		return nil, fmt.Errorf("error fetching columns: %w", err)
	}

	cmap := make(map[string]DBColumn)
	if err := scanDiscoveredColumnRows(rows, dbtype, blockList, cmap); err != nil {
		return nil, err
	}

	return enrichAndCollectColumns(ctx, db, dbtype, cmap), nil
}

func discoverPostgresColumns(ctx context.Context, db *sql.DB, blockList []string) ([]DBColumn, error) {
	cmap := make(map[string]DBColumn)
	var hasConstraints bool

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		if err := queryAndScanDiscoveredColumns(gctx, db, "postgres", blockList, cmap, postgresColumnsBasicStmt); err != nil {
			return fmt.Errorf("error fetching postgres columns: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		v, err := postgresHasConstraints(gctx, db)
		if err != nil {
			return fmt.Errorf("error checking postgres constraints: %w", err)
		}
		hasConstraints = v
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}
	if hasConstraints {
		constraints := make(map[string]DBColumn)
		if err := queryAndScanDiscoveredColumns(ctx, db, "postgres", blockList, constraints, postgresConstraintColumnsStmt); err != nil {
			return nil, fmt.Errorf("error fetching postgres constraints: %w", err)
		}
		mergeDiscoveredColumnMaps(cmap, constraints)
	}

	return enrichAndCollectColumns(ctx, db, "postgres", cmap), nil
}

func postgresHasConstraints(ctx context.Context, db *sql.DB) (bool, error) {
	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	var n int
	start := time.Now()
	err := db.QueryRowContext(qctx, postgresConstraintsCountStmt).Scan(&n)
	recordDiscoveryQuery(ctx, "constraint_preflight", postgresConstraintsCountStmt, time.Since(start))
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func discoverMySQLColumns(ctx context.Context, db *sql.DB, dbtype string, blockList []string) ([]DBColumn, error) {
	cmap := make(map[string]DBColumn)
	basicStmt := mysqlColumnsBasicStmt
	countStmt := mysqlConstraintsCountStmt
	constraintStmt := mysqlConstraintColumnsStmt
	if dbtype == "mariadb" {
		basicStmt = mariadbColumnsBasicStmt
		countStmt = mariadbConstraintsCountStmt
		constraintStmt = mariadbConstraintColumnsStmt
	}

	var hasConstraints bool
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		if err := queryAndScanDiscoveredColumns(gctx, db, dbtype, blockList, cmap, basicStmt); err != nil {
			return fmt.Errorf("error fetching %s columns: %w", dbtype, err)
		}
		return nil
	})
	g.Go(func() error {
		v, err := mysqlHasConstraints(gctx, db, countStmt)
		if err != nil {
			return fmt.Errorf("error checking %s constraints: %w", dbtype, err)
		}
		hasConstraints = v
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}
	if hasConstraints {
		constraints := make(map[string]DBColumn)
		if err := queryAndScanDiscoveredColumns(ctx, db, dbtype, blockList, constraints, constraintStmt); err != nil {
			return nil, fmt.Errorf("error fetching %s constraints: %w", dbtype, err)
		}
		mergeDiscoveredColumnMaps(cmap, constraints)
	}

	return enrichAndCollectColumns(ctx, db, dbtype, cmap), nil
}

func mysqlHasConstraints(ctx context.Context, db *sql.DB, stmt string) (bool, error) {
	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	var n int
	start := time.Now()
	err := db.QueryRowContext(qctx, stmt).Scan(&n)
	recordDiscoveryQuery(ctx, "constraint_preflight", stmt, time.Since(start))
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func discoverSnowflakeColumns(ctx context.Context, db *sql.DB, blockList []string) ([]DBColumn, error) {
	if snowflakeUseShowDiscovery() {
		if cols, err := discoverSnowflakeColumnsViaShow(ctx, db, blockList, true); err == nil && len(cols) != 0 {
			return cols, nil
		}
	}

	cols, showIncludeKeys, err := discoverSnowflakeColumnsViaInformationSchema(ctx, db, blockList)
	if err == nil && len(cols) != 0 {
		return cols, nil
	}
	if errors.Is(err, errSnowflakeNoUserSchemas) {
		if cols, err := discoverSnowflakeColumnsViaBasic(ctx, db, blockList); err == nil && len(cols) != 0 {
			return cols, nil
		}
	}
	if cols, err := discoverSnowflakeColumnsViaShow(ctx, db, blockList, showIncludeKeys); err == nil {
		if len(cols) != 0 {
			return cols, nil
		}
	}

	return discoverSnowflakeColumnsViaBasic(ctx, db, blockList)
}

func discoverSnowflakeColumnsViaBasic(ctx context.Context, db *sql.DB, blockList []string) ([]DBColumn, error) {
	cmap := make(map[string]DBColumn)
	if err := queryAndScanDiscoveredColumns(ctx, db, "snowflake", blockList, cmap, snowflakeColumnsBasicStmt); err != nil {
		return nil, fmt.Errorf("error fetching columns: %w", err)
	}
	return enrichAndCollectColumns(ctx, db, "snowflake", cmap), nil
}

func discoverSnowflakeColumnsViaInformationSchema(
	ctx context.Context,
	db *sql.DB,
	blockList []string,
) ([]DBColumn, bool, error) {
	schemas, err := discoverSnowflakeUserSchemas(ctx, db)
	if err != nil {
		return nil, true, fmt.Errorf("error fetching snowflake schemas: %w", err)
	}
	if len(schemas) == 0 {
		return nil, true, errSnowflakeNoUserSchemas
	}

	cmap := make(map[string]DBColumn)
	var hasOverrides bool

	hasOverrides, err = snowflakeFKMetadataExists(ctx, db)
	if err != nil {
		return nil, true, fmt.Errorf("error checking snowflake _gj_fk_metadata: %w", err)
	}

	var anyConstraints bool
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(4)
	for _, schema := range schemas {
		schema := schema
		g.Go(func() error {
			local := make(map[string]DBColumn)
			if err := queryAndScanDiscoveredColumns(gctx, db, "snowflake", blockList, local, snowflakeColumnsStmt, schema); err != nil {
				return fmt.Errorf("error fetching snowflake columns for schema %q: %w", schema, err)
			}

			hasConstraints, err := snowflakeHasConstraints(gctx, db, schema)
			if err != nil {
				return fmt.Errorf("error checking snowflake constraints for schema %q: %w", schema, err)
			}
			if hasConstraints {
				constraints := make(map[string]DBColumn)
				fkeys := make(map[string]DBColumn)
				cg, cgctx := errgroup.WithContext(gctx)
				cg.Go(func() error {
					if err := queryAndScanDiscoveredColumns(cgctx, db, "snowflake", blockList, constraints, snowflakeConstraintColumnsStmt, schema); err != nil {
						return fmt.Errorf("error fetching snowflake constraints: %w", err)
					}
					return nil
				})
				cg.Go(func() error {
					if err := queryAndScanDiscoveredColumns(cgctx, db, "snowflake", blockList, fkeys, snowflakeForeignKeysStmt, schema); err != nil {
						return fmt.Errorf("error fetching snowflake foreign keys: %w", err)
					}
					return nil
				})
				if err := cg.Wait(); err != nil {
					return err
				}
				mergeDiscoveredColumnMaps(local, constraints)
				mergeDiscoveredColumnMaps(local, fkeys)
			}

			mu.Lock()
			mergeDiscoveredColumnMaps(cmap, local)
			if hasConstraints {
				anyConstraints = true
			}
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, anyConstraints, err
	}

	if hasOverrides {
		if err := queryAndScanDiscoveredColumns(ctx, db, "snowflake", blockList, cmap, snowflakeFKMetadataStmt); err != nil {
			return nil, anyConstraints, fmt.Errorf("error fetching snowflake _gj_fk_metadata: %w", err)
		}
	}
	if len(cmap) == 0 {
		return nil, anyConstraints, errSnowflakeNoColumns
	}

	return enrichAndCollectColumns(ctx, db, "snowflake", cmap), anyConstraints, nil
}

func snowflakeHasConstraints(ctx context.Context, db *sql.DB, schema string) (bool, error) {
	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	var n int
	start := time.Now()
	err := db.QueryRowContext(qctx, snowflakeConstraintsCountStmt, schema).Scan(&n)
	recordDiscoveryQuery(ctx, "constraint_preflight", snowflakeConstraintsCountStmt, time.Since(start))
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func snowflakeFKMetadataExists(ctx context.Context, db *sql.DB) (bool, error) {
	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	var n int
	start := time.Now()
	err := db.QueryRowContext(qctx, snowflakeFKMetadataExistsStmt).Scan(&n)
	recordDiscoveryQuery(ctx, "constraint_preflight", snowflakeFKMetadataExistsStmt, time.Since(start))
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func discoverBigQueryColumns(ctx context.Context, db *sql.DB, blockList []string) ([]DBColumn, error) {
	cmap := make(map[string]DBColumn)
	var hasConstraints bool

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		if err := queryAndScanDiscoveredColumns(gctx, db, "bigquery", blockList, cmap, bigqueryColumnsStmt); err != nil {
			return fmt.Errorf("error fetching columns: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		v, err := bigQueryHasConstraints(gctx, db)
		if err != nil {
			return fmt.Errorf("error checking bigquery constraints: %w", err)
		}
		hasConstraints = v
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}
	if hasConstraints {
		pkeys := make(map[string]DBColumn)
		fkeys := make(map[string]DBColumn)
		g, gctx = errgroup.WithContext(ctx)
		g.Go(func() error {
			if err := queryAndScanDiscoveredColumns(gctx, db, "bigquery", blockList, pkeys, bigqueryPrimaryKeysStmt); err != nil {
				return fmt.Errorf("error fetching bigquery primary keys: %w", err)
			}
			return nil
		})
		g.Go(func() error {
			if err := queryAndScanDiscoveredColumns(gctx, db, "bigquery", blockList, fkeys, bigqueryForeignKeysStmt); err != nil {
				return fmt.Errorf("error fetching bigquery foreign keys: %w", err)
			}
			return nil
		})
		if err := g.Wait(); err != nil {
			return nil, err
		}
		mergeDiscoveredColumnMaps(cmap, pkeys)
		mergeDiscoveredColumnMaps(cmap, fkeys)
	}

	return enrichAndCollectColumns(ctx, db, "bigquery", cmap), nil
}

func snowflakeUseShowDiscovery() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("GRAPHJIN_SNOWFLAKE_DISCOVERY")), "show")
}

func snowflakeSkipClusteringDiscovery() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GRAPHJIN_SNOWFLAKE_DISCOVERY_CLUSTERING"))) {
	case "0", "false", "off", "skip", "disabled", "disable":
		return true
	default:
		return false
	}
}

func bigQueryHasConstraints(ctx context.Context, db *sql.DB) (bool, error) {
	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	var n int
	start := time.Now()
	err := db.QueryRowContext(qctx, bigqueryConstraintsCountStmt).Scan(&n)
	recordDiscoveryQuery(ctx, "constraint_preflight", bigqueryConstraintsCountStmt, time.Since(start))
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func queryAndScanDiscoveredColumns(
	ctx context.Context,
	db *sql.DB,
	dbtype string,
	blockList []string,
	cmap map[string]DBColumn,
	stmt string,
	args ...any,
) error {
	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	start := time.Now()
	rows, err := db.QueryContext(qctx, stmt, args...)
	recordDiscoveryQuery(ctx, "schema_metadata", stmt, time.Since(start))
	if err != nil {
		return err
	}
	return scanDiscoveredColumnRows(rows, dbtype, blockList, cmap)
}

func scanDiscoveredColumnRows(rows *sql.Rows, dbtype string, blockList []string, cmap map[string]DBColumn) error {
	defer rows.Close()

	i := len(cmap)
	// we have to rescan and update columns to overcome
	// weird bugs in mysql like joins with information_schema
	// don't work in 8.0.22 etc.
	for rows.Next() {
		var c DBColumn
		c.ID = int32(i)

		err := rows.Scan(&c.Schema,
			&c.Table,
			&c.Name,
			&c.Type,
			&c.NotNull,
			&c.PrimaryKey,
			&c.UniqueKey,
			&c.Array,
			&c.FullText,
			&c.FKeySchema,
			&c.FKeyTable,
			&c.FKeyCol)

		if err != nil {
			return err
		}

		c.FKeySchema = strings.TrimSpace(c.FKeySchema)
		c.FKeyTable = strings.TrimSpace(c.FKeyTable)
		c.FKeyCol = strings.TrimSpace(c.FKeyCol)

		if dbtype == "mssql" || dbtype == "snowflake" || dbtype == "bigquery" {
			c.OrigName = c.Name
			c.OrigTable = c.Table
			c.OrigSchema = c.Schema
			c.OrigFKeyTable = c.FKeyTable
			c.OrigFKeySchema = c.FKeySchema
			c.OrigFKeyCol = c.FKeyCol
		}

		if dbtype == "sqlite" || dbtype == "oracle" || dbtype == "mssql" || dbtype == "snowflake" || dbtype == "bigquery" {
			c.Name = util.ToSnake(c.Name)
			c.Table = strings.ToLower(c.Table)
			c.Schema = strings.ToLower(c.Schema)
			c.Type = strings.ToLower(c.Type)
			c.FKeyTable = strings.ToLower(c.FKeyTable)
			c.FKeySchema = strings.ToLower(c.FKeySchema)
			c.FKeyCol = util.ToSnake(c.FKeyCol)
		}

		k := (c.Schema + ":" + c.Table + ":" + c.Name)
		v, ok := cmap[k]
		if !ok {
			v = c
			v.ID = int32(len(cmap))
			if strings.HasPrefix(v.Table, "_gj_") {
				continue
			}
			v.Blocked = isInList(v.Name, blockList)
		}
		if c.Type != "" {
			v.Type = c.Type
		}
		if c.PrimaryKey {
			v.PrimaryKey = true
			v.UniqueKey = true
		}
		if c.NotNull {
			v.NotNull = true
		}
		if c.UniqueKey {
			v.UniqueKey = true
		}
		if c.Array {
			v.Array = true
		}
		if c.FullText {
			v.FullText = true
		}
		if c.FKeySchema != "" {
			v.FKeySchema = c.FKeySchema
		}
		if c.FKeyTable != "" {
			v.FKeyTable = c.FKeyTable
		}
		if c.FKeyCol != "" {
			v.FKeyCol = c.FKeyCol
		}
		if v.FKeySchema == v.Schema && v.FKeyTable == v.Table {
			v.FKRecursive = true
		}
		cmap[k] = v
		i++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error scanning columns: %w", err)
	}
	return nil
}

func mergeDiscoveredColumnMaps(dst, src map[string]DBColumn) {
	for k, c := range src {
		v, ok := dst[k]
		if !ok {
			c.ID = int32(len(dst))
			dst[k] = c
			continue
		}
		if c.Type != "" {
			v.Type = c.Type
		}
		if c.PrimaryKey {
			v.PrimaryKey = true
			v.UniqueKey = true
		}
		if c.NotNull {
			v.NotNull = true
		}
		if c.UniqueKey {
			v.UniqueKey = true
		}
		if c.Array {
			v.Array = true
		}
		if c.FullText {
			v.FullText = true
		}
		if c.FKeySchema != "" {
			v.FKeySchema = c.FKeySchema
		}
		if c.FKeyTable != "" {
			v.FKeyTable = c.FKeyTable
		}
		if c.FKeyCol != "" {
			v.FKeyCol = c.FKeyCol
		}
		if c.OrigName != "" && v.OrigName == "" {
			v.OrigName = c.OrigName
		}
		if c.OrigTable != "" && v.OrigTable == "" {
			v.OrigTable = c.OrigTable
		}
		if c.OrigSchema != "" && v.OrigSchema == "" {
			v.OrigSchema = c.OrigSchema
		}
		if c.OrigFKeySchema != "" {
			v.OrigFKeySchema = c.OrigFKeySchema
		}
		if c.OrigFKeyTable != "" {
			v.OrigFKeyTable = c.OrigFKeyTable
		}
		if c.OrigFKeyCol != "" {
			v.OrigFKeyCol = c.OrigFKeyCol
		}
		if v.FKeySchema == v.Schema && v.FKeyTable == v.Table {
			v.FKRecursive = true
		}
		dst[k] = v
	}
}

func enrichAndCollectColumns(ctx context.Context, db *sql.DB, dbtype string, cmap map[string]DBColumn) []DBColumn {
	// View PK detection: views lack PK constraints in system catalogs, so the
	// main column query reports primary_key=false for all view columns. We run
	// a supplementary query to trace view columns back to source base table PKs.
	//
	// All paths are gated behind a cheap preflight check ("does this DB have
	// user-visible views?") to avoid running expensive catalog queries when
	// no views exist — the common case for most deployments.
	if hasUserViews(ctx, db, dbtype) {
		// Layer 1: SQL-level tracing via system catalogs (best accuracy).
		// Supported: PostgreSQL, MSSQL, Oracle, MySQL 8.0+.
		// Not supported: MariaDB, SQLite, Snowflake (no system catalog for view column tracing).
		var viewPKStmt string
		var needsNormalize bool
		switch dbtype {
		case "postgres", "":
			viewPKStmt = postgresViewPKsStmt
		case "mssql":
			viewPKStmt = mssqlViewPKsStmt
			needsNormalize = true
		case "oracle":
			viewPKStmt = oracleViewPKsStmt
			needsNormalize = true
		case "mysql":
			viewPKStmt = mysqlViewPKsStmt
		}
		if viewPKStmt != "" {
			qctx2, cancel2 := context.WithTimeout(ctx, introspectionQueryTimeout)
			defer cancel2()
			rows2, err := db.QueryContext(qctx2, viewPKStmt)
			if err == nil {
				defer rows2.Close()
				for rows2.Next() {
					var schema, table, column string
					if err := rows2.Scan(&schema, &table, &column); err != nil {
						continue
					}
					if needsNormalize {
						column = util.ToSnake(column)
						table = strings.ToLower(table)
						schema = strings.ToLower(schema)
					}

					k := schema + ":" + table + ":" + column
					if v, ok := cmap[k]; ok && !v.PrimaryKey {
						v.PrimaryKey = true
						v.UniqueKey = true
						cmap[k] = v
					}
				}
			}
			// Silently ignore errors — falls back to heuristic below
		}

		// Layer 2: Code-level heuristic fallback. For databases without SQL-level
		// view PK detection (MariaDB, SQLite, Snowflake) or when the SQL query fails,
		// infer PKs by matching view columns against base table PKs in the same schema.
		inferViewPKsFromBaseTables(cmap)
	}

	var cols []DBColumn
	for _, c := range cmap {
		cols = append(cols, c)
	}

	return cols
}

// hasUserViews returns true if the database has at least one user-visible view.
// This is a cheap preflight check to avoid running expensive view PK detection
// queries (which may compile every view) when no views exist. Returns false on
// error or for unsupported database types — the view-PK enrichment is non-essential
// and users can override via config.
func hasUserViews(ctx context.Context, db *sql.DB, dbtype string) bool {
	var stmt string
	switch dbtype {
	case "mssql":
		stmt = mssqlHasViewsStmt
	case "postgres", "":
		stmt = `SELECT 1 FROM pg_class c JOIN pg_namespace n ON c.relnamespace = n.oid WHERE c.relkind IN ('v','m') AND n.nspname NOT IN ('pg_catalog','information_schema','_graphjin') LIMIT 1`
	case "mysql":
		stmt = `SELECT 1 FROM information_schema.VIEWS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_SCHEMA NOT IN ('mysql','information_schema','performance_schema','sys') LIMIT 1`
	case "mariadb":
		stmt = `SELECT 1 FROM information_schema.VIEWS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_SCHEMA NOT IN ('mysql','information_schema','performance_schema','sys') LIMIT 1`
	case "oracle":
		stmt = `SELECT 1 FROM all_views WHERE owner NOT IN ('SYS','SYSTEM','OUTLN','DBSNMP','APPQOSSYS','XDB','WMSYS','CTXSYS','MDSYS','ORDSYS','ORDDATA') AND ROWNUM = 1`
	case "sqlite":
		stmt = `SELECT 1 FROM sqlite_master WHERE type='view' LIMIT 1`
	default:
		return false
	}

	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	var n int
	start := time.Now()
	err := db.QueryRowContext(qctx, stmt).Scan(&n)
	recordDiscoveryQuery(ctx, "view_preflight", stmt, time.Since(start))
	if err != nil {
		return false
	}
	return n > 0
}

// inferViewPKsFromBaseTables is a universal fallback for view PK detection.
// For any table/view that has NO PK columns, it finds the "best matching"
// base table by counting how many non-PK columns overlap. If exactly one
// base table has the highest overlap AND all its PK columns appear in the
// view, those view columns are marked as PKs.
func inferViewPKsFromBaseTables(cmap map[string]DBColumn) {
	type st struct{ schema, table string }

	// Step 1: Build per-table column sets and PK sets
	tableCols := make(map[st]map[string]bool)
	tablePKs := make(map[st][]string)
	hasPK := make(map[st]bool)

	for _, c := range cmap {
		key := st{c.Schema, c.Table}
		if tableCols[key] == nil {
			tableCols[key] = make(map[string]bool)
		}
		tableCols[key][c.Name] = true
		if c.PrimaryKey {
			hasPK[key] = true
			tablePKs[key] = append(tablePKs[key], c.Name)
		}
	}

	// Step 2: For each table with no PK, find the best matching base table
	for viewKey, viewCols := range tableCols {
		if hasPK[viewKey] {
			continue
		}
		if strings.HasPrefix(viewKey.table, "_gj_") {
			continue
		}

		type candidate struct {
			table   st
			overlap int
			pkCols  []string
		}
		var best candidate
		var tied bool

		for baseKey, basePKCols := range tablePKs {
			if baseKey.schema != viewKey.schema || baseKey == viewKey {
				continue
			}

			// Check if ALL PK columns of this base table appear in the view
			allPKsPresent := true
			for _, pk := range basePKCols {
				if !viewCols[pk] {
					allPKsPresent = false
					break
				}
			}
			if !allPKsPresent {
				continue
			}

			// Count non-PK column overlap
			overlap := 0
			baseCols := tableCols[baseKey]
			for col := range baseCols {
				if !containsStr(basePKCols, col) && viewCols[col] {
					overlap++
				}
			}

			if overlap == 0 {
				continue
			}

			if overlap > best.overlap {
				best = candidate{baseKey, overlap, basePKCols}
				tied = false
			} else if overlap == best.overlap && best.table != baseKey {
				tied = true
			}
		}

		// Only apply if we have an unambiguous winner
		if best.overlap > 0 && !tied {
			for _, pkCol := range best.pkCols {
				k := viewKey.schema + ":" + viewKey.table + ":" + pkCol
				if v, ok := cmap[k]; ok && !v.PrimaryKey {
					v.PrimaryKey = true
					v.UniqueKey = true
					cmap[k] = v
				}
			}
		}
	}
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// inferDefaultSchema picks the schema that contains the most distinct tables
// from the discovered columns. Used as a fallback when the database reports
// an empty default schema (e.g., PostgreSQL with search_path=”).
func inferDefaultSchema(cols []DBColumn) string {
	type st struct{ schema, table string }
	seen := make(map[st]bool)
	counts := make(map[string]int)
	for _, c := range cols {
		if c.Schema == "" || strings.HasPrefix(c.Table, "_gj_") {
			continue
		}
		k := st{c.Schema, c.Table}
		if !seen[k] {
			seen[k] = true
			counts[c.Schema]++
		}
	}

	var best string
	var bestCount int
	for schema, n := range counts {
		if n > bestCount {
			best = schema
			bestCount = n
		}
	}
	return best
}

// DiscoverCompositeFKs returns metadata about composite (multi-column) foreign key
// constraints for the given database type.
//
// Composite FK discovery is a best-effort enrichment: if the query errors or
// times out, we return (nil, nil) and let the rest of the schema load normally.
// Single-column FKs (the overwhelmingly common case) come from DiscoverColumns
// and are unaffected. This prevents a slow/broken data-dictionary query from
// failing the whole NewGraphJin init.
func DiscoverCompositeFKs(ctx context.Context, db *sql.DB, dbtype string) ([]CompositeFKInfo, error) {
	var (
		result []CompositeFKInfo
		err    error
	)
	switch dbtype {
	case "postgres", "":
		result, err = discoverCompositeFKsPostgres(ctx, db)
	case "mysql":
		result, err = discoverCompositeFKsCSV(ctx, db, dbtype, compositeFKQueryMySQL)
	case "mariadb":
		result, err = discoverCompositeFKsCSV(ctx, db, dbtype, compositeFKQueryMySQL) // identical to MySQL
	case "sqlite":
		result, err = discoverCompositeFKsCSV(ctx, db, dbtype, compositeFKQuerySQLite)
	case "oracle":
		result, err = discoverCompositeFKsCSV(ctx, db, dbtype, compositeFKQueryOracle)
	case "mssql":
		result, err = discoverCompositeFKsCSV(ctx, db, dbtype, compositeFKQueryMSSQL)
	case "snowflake":
		result, err = discoverSnowflakeCompositeFKsViaShow(ctx, db)
	case "bigquery":
		result, err = discoverCompositeFKsCSV(ctx, db, dbtype, compositeFKQueryBigQuery)
	default:
		return nil, nil
	}
	if err != nil {
		// Non-fatal: log nothing here (caller has no logger), just drop the
		// enrichment. Single-column FKs are unaffected.
		return nil, nil
	}
	return result, nil
}

// compositeFKQueryMySQL is scoped to the current DATABASE() to avoid scanning
// every schema on the server — MySQL's information_schema views are synthesized
// on demand and are prohibitively slow when unfiltered. The join also matches
// on table_name so constraint-name collisions across tables cannot fan out.
const compositeFKQueryMySQL = `
SELECT kcu.table_schema, kcu.table_name, kcu.constraint_name,
       GROUP_CONCAT(kcu.column_name ORDER BY kcu.ordinal_position) AS local_columns,
       kcu.referenced_table_schema, kcu.referenced_table_name,
       GROUP_CONCAT(kcu.referenced_column_name ORDER BY kcu.ordinal_position) AS fkey_columns
FROM information_schema.key_column_usage kcu
JOIN information_schema.table_constraints tc
  ON kcu.constraint_name = tc.constraint_name
 AND kcu.table_schema = tc.table_schema
 AND kcu.table_name = tc.table_name
WHERE tc.constraint_type = 'FOREIGN KEY'
  AND kcu.table_schema = DATABASE()
GROUP BY kcu.table_schema, kcu.table_name, kcu.constraint_name,
         kcu.referenced_table_schema, kcu.referenced_table_name
HAVING COUNT(*) > 1`

const compositeFKQuerySQLite = `
SELECT 'main' AS schema_name, m.name AS table_name,
       CAST(fk.id AS TEXT) AS constraint_name,
       GROUP_CONCAT(fk."from", ',') AS local_columns,
       'main' AS fkey_schema,
       fk."table" AS fkey_table,
       GROUP_CONCAT(fk."to", ',') AS fkey_columns
FROM sqlite_master m
CROSS JOIN pragma_foreign_key_list(m.name) fk
WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%' AND m.name NOT LIKE '_gj_%'
GROUP BY m.name, fk.id, fk."table"
HAVING COUNT(*) > 1`

// compositeFKQueryOracle discovers composite foreign keys owned by the current
// session user.
//
// Local side  — user_constraints / user_cons_columns: owns only current-user
// constraints, no security-predicate overhead, 10-100× faster than all_*.
//
// Referenced side — all_constraints / all_cons_columns: the target table may
// live in a different schema (cross-schema FK). These are single indexed
// key-lookups on r_constraint_name, not full scans, so the cost is O(1) per
// FK constraint regardless of how many system schemas exist.
const compositeFKQueryOracle = `
SELECT USER AS owner, uc.table_name, uc.constraint_name,
       LISTAGG(ucc.column_name, ',') WITHIN GROUP (ORDER BY ucc.position) AS local_columns,
       LOWER(r_ac.owner) AS fkey_schema, r_ac.table_name AS fkey_table,
       LISTAGG(r_acc.column_name, ',') WITHIN GROUP (ORDER BY r_acc.position) AS fkey_columns
FROM user_constraints uc
JOIN user_cons_columns ucc
  ON uc.constraint_name = ucc.constraint_name
JOIN all_constraints r_ac
  ON uc.r_constraint_name = r_ac.constraint_name
JOIN all_cons_columns r_acc
  ON r_ac.constraint_name = r_acc.constraint_name
 AND r_ac.owner           = r_acc.owner
 AND ucc.position         = r_acc.position
WHERE uc.constraint_type = 'R'
GROUP BY uc.table_name, uc.constraint_name, r_ac.owner, r_ac.table_name
HAVING COUNT(*) > 1`

// compositeFKQueryMSSQL filters system and role schemas on BOTH sides of the
// FK so the result set is consistent with mssql_columns.sql. sys.tables is
// user-tables-only so sys/INFORMATION_SCHEMA are self-filtered, but CDC
// (Change Data Capture) and audit schemas expose user-visible tables that
// would otherwise leak through.
const compositeFKQueryMSSQL = `
SELECT s.name, t.name, OBJECT_NAME(fkc.constraint_object_id),
       STRING_AGG(c.name, ',') AS local_columns,
       rs.name, rt.name,
       STRING_AGG(rc.name, ',') AS fkey_columns
FROM sys.foreign_key_columns fkc
JOIN sys.columns c ON fkc.parent_object_id = c.object_id AND fkc.parent_column_id = c.column_id
JOIN sys.columns rc ON fkc.referenced_object_id = rc.object_id AND fkc.referenced_column_id = rc.column_id
JOIN sys.tables t ON fkc.parent_object_id = t.object_id
JOIN sys.schemas s ON t.schema_id = s.schema_id
JOIN sys.tables rt ON fkc.referenced_object_id = rt.object_id
JOIN sys.schemas rs ON rt.schema_id = rs.schema_id
WHERE s.name NOT IN (
    'sys', 'INFORMATION_SCHEMA', 'guest', 'cdc',
    'db_owner', 'db_accessadmin', 'db_securityadmin', 'db_ddladmin',
    'db_backupoperator', 'db_datareader', 'db_datawriter',
    'db_denydatareader', 'db_denydatawriter'
)
AND rs.name NOT IN (
    'sys', 'INFORMATION_SCHEMA', 'guest', 'cdc',
    'db_owner', 'db_accessadmin', 'db_securityadmin', 'db_ddladmin',
    'db_backupoperator', 'db_datareader', 'db_datawriter',
    'db_denydatareader', 'db_denydatawriter'
)
GROUP BY s.name, t.name, fkc.constraint_object_id, rs.name, rt.name
HAVING COUNT(*) > 1`

const compositeFKQuerySnowflake = `
SELECT table_schema, table_name,
       table_schema || ':' || table_name || ':' || foreign_table_name AS constraint_name,
       LISTAGG(column_name, ',') WITHIN GROUP (ORDER BY column_name) AS local_columns,
       foreign_table_schema, foreign_table_name,
       LISTAGG(foreign_column_name, ',') WITHIN GROUP (ORDER BY foreign_column_name) AS fkey_columns
FROM _gj_fk_metadata
GROUP BY table_schema, table_name, foreign_table_schema, foreign_table_name
HAVING COUNT(*) > 1`

const compositeFKQueryBigQuery = `
SELECT fk_kcu.table_schema, fk_kcu.table_name, fk_kcu.constraint_name,
       STRING_AGG(fk_kcu.column_name, ',' ORDER BY fk_kcu.ordinal_position) AS local_columns,
       pk_kcu.table_schema AS fkey_schema,
       pk_kcu.table_name AS fkey_table,
       STRING_AGG(pk_kcu.column_name, ',' ORDER BY fk_kcu.ordinal_position) AS fkey_columns
FROM information_schema.key_column_usage fk_kcu
JOIN information_schema.table_constraints tc
  ON fk_kcu.constraint_catalog = tc.constraint_catalog
 AND fk_kcu.constraint_schema = tc.constraint_schema
 AND fk_kcu.constraint_name = tc.constraint_name
JOIN information_schema.constraint_column_usage ccu
  ON ccu.constraint_catalog = fk_kcu.constraint_catalog
 AND ccu.constraint_schema = fk_kcu.constraint_schema
 AND ccu.constraint_name = fk_kcu.constraint_name
JOIN information_schema.key_column_usage pk_kcu
  ON pk_kcu.table_schema = ccu.table_schema
 AND pk_kcu.table_name = ccu.table_name
 AND pk_kcu.ordinal_position = fk_kcu.position_in_unique_constraint
JOIN information_schema.table_constraints pk_tc
  ON pk_tc.constraint_catalog = pk_kcu.constraint_catalog
 AND pk_tc.constraint_schema = pk_kcu.constraint_schema
 AND pk_tc.constraint_name = pk_kcu.constraint_name
 AND pk_tc.constraint_type = 'PRIMARY KEY'
WHERE tc.constraint_type = 'FOREIGN KEY'
  AND (
    COALESCE(@@dataset_id, '') = ''
    OR LOWER(fk_kcu.table_schema) = LOWER(COALESCE(@@dataset_id, ''))
  )
GROUP BY fk_kcu.table_schema, fk_kcu.table_name, fk_kcu.constraint_name,
         pk_kcu.table_schema, pk_kcu.table_name
HAVING COUNT(*) > 1`

func discoverCompositeFKsPostgres(ctx context.Context, db *sql.DB) ([]CompositeFKInfo, error) {
	const query = `
SELECT
	n.nspname AS schema_name,
	c.relname AS table_name,
	co.conname AS constraint_name,
	array_agg(a.attname ORDER BY k.ord) AS local_columns,
	fn.nspname AS fkey_schema,
	fc.relname AS fkey_table,
	array_agg(fa.attname ORDER BY k.ord) AS fkey_columns
FROM pg_constraint co
	JOIN pg_class c ON c.oid = co.conrelid
	JOIN pg_namespace n ON n.oid = c.relnamespace
	JOIN pg_class fc ON fc.oid = co.confrelid
	JOIN pg_namespace fn ON fn.oid = fc.relnamespace
	CROSS JOIN LATERAL unnest(co.conkey, co.confkey) WITH ORDINALITY AS k(local_attnum, foreign_attnum, ord)
	JOIN pg_attribute a ON a.attrelid = co.conrelid AND a.attnum = k.local_attnum
	JOIN pg_attribute fa ON fa.attrelid = co.confrelid AND fa.attnum = k.foreign_attnum
WHERE co.contype = 'f'
	AND n.nspname NOT IN ('_graphjin', 'information_schema', 'pg_catalog')
	AND array_length(co.conkey, 1) > 1
GROUP BY n.nspname, c.relname, co.conname, fn.nspname, fc.relname`

	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(qctx, query)
	if err != nil {
		return nil, fmt.Errorf("error fetching composite FKs: %w", err)
	}
	defer rows.Close()

	var result []CompositeFKInfo
	for rows.Next() {
		var info CompositeFKInfo
		var localCols, fkeyCols []string
		if err := rows.Scan(
			&info.Schema, &info.Table, &info.ConstraintName,
			(*pgStringArray)(&localCols),
			&info.FKeySchema, &info.FKeyTable,
			(*pgStringArray)(&fkeyCols),
		); err != nil {
			return nil, fmt.Errorf("error scanning composite FK: %w", err)
		}
		info.LocalCols = localCols
		info.FKeyCols = fkeyCols
		result = append(result, info)
	}
	return result, rows.Err()
}

// discoverCompositeFKsCSV handles composite FK discovery for databases that return
// aggregated columns as comma-separated strings (MySQL, MariaDB, SQLite, Oracle, MSSQL, Snowflake).
func discoverCompositeFKsCSV(ctx context.Context, db *sql.DB, dbtype, query string) ([]CompositeFKInfo, error) {
	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(qctx, query)
	if err != nil {
		return nil, fmt.Errorf("error fetching composite FKs: %w", err)
	}
	defer rows.Close()

	normalize := dbtype == "oracle" || dbtype == "mssql" || dbtype == "snowflake" || dbtype == "bigquery"

	var result []CompositeFKInfo
	for rows.Next() {
		var info CompositeFKInfo
		var localCSV, fkeyCSV string
		if err := rows.Scan(
			&info.Schema, &info.Table, &info.ConstraintName,
			&localCSV,
			&info.FKeySchema, &info.FKeyTable,
			&fkeyCSV,
		); err != nil {
			return nil, fmt.Errorf("error scanning composite FK: %w", err)
		}
		info.LocalCols = strings.Split(localCSV, ",")
		info.FKeyCols = strings.Split(fkeyCSV, ",")

		if normalize {
			info.Schema = strings.ToLower(info.Schema)
			info.Table = strings.ToLower(info.Table)
			info.FKeySchema = strings.ToLower(info.FKeySchema)
			info.FKeyTable = strings.ToLower(info.FKeyTable)
			for i := range info.LocalCols {
				info.LocalCols[i] = strings.ToLower(util.ToSnake(strings.TrimSpace(info.LocalCols[i])))
			}
			for i := range info.FKeyCols {
				info.FKeyCols[i] = strings.ToLower(util.ToSnake(strings.TrimSpace(info.FKeyCols[i])))
			}
		}
		result = append(result, info)
	}
	return result, rows.Err()
}

// pgStringArray implements sql.Scanner for Postgres text[] columns.
type pgStringArray []string

func (a *pgStringArray) Scan(src interface{}) error {
	if src == nil {
		*a = nil
		return nil
	}
	var s string
	switch v := src.(type) {
	case []byte:
		s = string(v)
	case string:
		s = v
	default:
		return fmt.Errorf("pgStringArray: unsupported type %T", src)
	}
	// Parse Postgres array literal: {val1,val2,...}
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return fmt.Errorf("pgStringArray: invalid format %q", s)
	}
	inner := s[1 : len(s)-1]
	if inner == "" {
		*a = nil
		return nil
	}
	*a = strings.Split(inner, ",")
	return nil
}

// DiscoverFunctions returns the functions of a database
func DiscoverFunctions(ctx context.Context, db *sql.DB, dbtype string, blockList []string) ([]DBFunction, error) {
	var sqlStmt string

	switch dbtype {
	case "postgres", "":
		sqlStmt = postgresFunctionsStmt
	case "mysql":
		sqlStmt = mysqlFunctionsStmt
	case "mariadb":
		sqlStmt = mariadbFunctionsStmt
	case "sqlite":
		sqlStmt = sqliteFunctionsStmt
	case "oracle":
		sqlStmt = oracleFunctionsStmt
	case "mssql":
		sqlStmt = mssqlFunctionsStmt
	case "snowflake":
		// Snowflake emulator does not expose information_schema.functions consistently.
		// Return no discovered functions for now.
		return nil, nil
	case "bigquery":
		// BigQuery routines need dataset-qualified INFORMATION_SCHEMA queries.
		// Skip routine discovery until the BigQuery connector carries dataset scope.
		return nil, nil
	case "mongodb":
		// MongoDB doesn't have user-defined functions in the SQL sense
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported database type %q: supported types are postgres, mysql, mariadb, sqlite, oracle, mssql, snowflake, bigquery, mongodb", dbtype)
	}

	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(qctx, sqlStmt)
	if err != nil {
		return nil, fmt.Errorf("error fetching functions: %w", err)
	}
	defer rows.Close()

	var funcs []DBFunction
	fm := make(map[string]int)

	for rows.Next() {
		var fid, fs, fn, ft string
		var pn, pt, pk sql.NullString
		var pid sql.NullInt64

		err = rows.Scan(&fid, &fs, &fn, &ft, &pid, &pn, &pt, &pk)
		if err != nil {
			return nil, err
		}

		if isInList(fn, blockList) {
			continue
		}

		i, ok := fm[fid]
		if !ok {
			funcs = append(funcs, DBFunction{Schema: fs, Name: fn, Type: ft})
			i = len(funcs) - 1
			fm[fid] = i
		}

		pidVal := 0
		if pid.Valid {
			pidVal = int(pid.Int64)
		}
		param := DBFuncParam{ID: pidVal, Name: pn.String, Type: pt.String}

		if strings.HasSuffix(pt.String, "[]") {
			param.Array = true
		}

		switch pk.String {
		case "IN", "in":
			funcs[i].Inputs = append(funcs[i].Inputs, param)
		case "OUT", "out":
			funcs[i].Outputs = append(funcs[i].Outputs, param)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error scanning functions: %w", err)
	}

	return funcs, nil
}

// isInList checks if a value is in a list
func isInList(val string, s []string) bool {
	for _, v := range s {
		regex := fmt.Sprintf("^%s$", v)
		if matched, _ := regexp.MatchString(regex, val); matched {
			return true
		}
	}
	return false
}

func discoverSnowflakeCompositeFKsViaShow(ctx context.Context, db *sql.DB) ([]CompositeFKInfo, error) {
	schemas, err := discoverSnowflakeUserSchemas(ctx, db)
	if err != nil || len(schemas) == 0 {
		schemas = []string{""}
	}

	aggQuery := `
SELECT "fk_schema_name" AS table_schema,
       "fk_table_name" AS table_name,
       "fk_name" AS constraint_name,
       LISTAGG("fk_column_name", ',') WITHIN GROUP (ORDER BY "key_sequence") AS local_columns,
       "pk_schema_name" AS fkey_schema,
       "pk_table_name" AS fkey_table,
       LISTAGG("pk_column_name", ',') WITHIN GROUP (ORDER BY "key_sequence") AS fkey_columns
FROM TABLE(RESULT_SCAN(?))
GROUP BY "fk_schema_name", "fk_table_name", "fk_name", "pk_schema_name", "pk_table_name"
HAVING COUNT(*) > 1`

	var result []CompositeFKInfo
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(4)

	for _, schema := range schemas {
		schema := schema
		g.Go(func() error {
			conn, err := db.Conn(gctx)
			if err != nil {
				return err
			}
			defer conn.Close()

			sctx, cancel := context.WithTimeout(gctx, introspectionQueryTimeout)
			stmt := snowflakeShowInSchemaStmt("SHOW IMPORTED KEYS IN SCHEMA", schema)
			start := time.Now()
			if _, err := conn.ExecContext(sctx, stmt); err != nil {
				recordDiscoveryQuery(ctx, "schema_metadata", stmt, time.Since(start))
				cancel()
				return err
			}
			recordDiscoveryQuery(ctx, "schema_metadata", stmt, time.Since(start))
			var qid string
			start = time.Now()
			if err := conn.QueryRowContext(sctx, "SELECT LAST_QUERY_ID()").Scan(&qid); err != nil {
				recordDiscoveryQuery(ctx, "schema_metadata", "SELECT LAST_QUERY_ID()", time.Since(start))
				cancel()
				return err
			}
			recordDiscoveryQuery(ctx, "schema_metadata", "SELECT LAST_QUERY_ID()", time.Since(start))
			cancel()

			var local []CompositeFKInfo
			qctx, qcancel := context.WithTimeout(gctx, introspectionQueryTimeout)
			start = time.Now()
			rows, err := conn.QueryContext(qctx, aggQuery, qid)
			recordDiscoveryQuery(ctx, "schema_metadata", aggQuery, time.Since(start))
			if err != nil {
				qcancel()
				return err
			}
			for rows.Next() {
				var info CompositeFKInfo
				var localCSV, fkeyCSV string
				if err := rows.Scan(
					&info.Schema, &info.Table, &info.ConstraintName,
					&localCSV,
					&info.FKeySchema, &info.FKeyTable,
					&fkeyCSV,
				); err != nil {
					rows.Close()
					qcancel()
					return err
				}
				info.LocalCols = strings.Split(localCSV, ",")
				info.FKeyCols = strings.Split(fkeyCSV, ",")
				info.Schema = strings.ToLower(info.Schema)
				info.Table = strings.ToLower(info.Table)
				info.FKeySchema = strings.ToLower(info.FKeySchema)
				info.FKeyTable = strings.ToLower(info.FKeyTable)
				for i := range info.LocalCols {
					info.LocalCols[i] = strings.ToLower(util.ToSnake(strings.TrimSpace(info.LocalCols[i])))
				}
				for i := range info.FKeyCols {
					info.FKeyCols[i] = strings.ToLower(util.ToSnake(strings.TrimSpace(info.FKeyCols[i])))
				}
				local = append(local, info)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				qcancel()
				return err
			}
			rows.Close()
			qcancel()
			mu.Lock()
			result = append(result, local...)
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return result, nil
}

func discoverSnowflakeColumnsViaShow(ctx context.Context, db *sql.DB, blockList []string, includeKeys bool) ([]DBColumn, error) {
	schemas, err := discoverSnowflakeUserSchemas(ctx, db)
	if err != nil || len(schemas) == 0 {
		schemas = []string{""}
	}

	cmap := make(map[string]DBColumn)
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(4)

	for _, schema := range schemas {
		schema := schema
		g.Go(func() error {
			conn, err := db.Conn(gctx)
			if err != nil {
				return err
			}
			defer conn.Close()

			runShow := func(stmt string) (string, error) {
				sctx, cancel := context.WithTimeout(gctx, introspectionQueryTimeout)
				defer cancel()
				start := time.Now()
				if _, err := conn.ExecContext(sctx, stmt); err != nil {
					recordDiscoveryQuery(ctx, "schema_metadata", stmt, time.Since(start))
					return "", err
				}
				recordDiscoveryQuery(ctx, "schema_metadata", stmt, time.Since(start))
				var qid string
				start = time.Now()
				if err := conn.QueryRowContext(sctx, "SELECT LAST_QUERY_ID()").Scan(&qid); err != nil {
					recordDiscoveryQuery(ctx, "schema_metadata", "SELECT LAST_QUERY_ID()", time.Since(start))
					return "", err
				}
				recordDiscoveryQuery(ctx, "schema_metadata", "SELECT LAST_QUERY_ID()", time.Since(start))
				return qid, nil
			}

			local := make(map[string]DBColumn)
			colsQID, err := runShow(snowflakeShowInSchemaStmt("SHOW COLUMNS IN SCHEMA", schema))
			if err != nil {
				return fmt.Errorf("snowflake SHOW COLUMNS: %w", err)
			}

			if !includeKeys {
				qctx, cancel := context.WithTimeout(gctx, introspectionQueryTimeout)
				start := time.Now()
				rows, err := conn.QueryContext(qctx, snowflakeColumnsShowBasicStmt, colsQID)
				recordDiscoveryQuery(ctx, "schema_metadata", snowflakeColumnsShowBasicStmt, time.Since(start))
				if err != nil {
					cancel()
					return err
				}
				if err := scanDiscoveredColumnRows(rows, "snowflake", blockList, local); err != nil {
					cancel()
					return err
				}
				cancel()

				mu.Lock()
				mergeDiscoveredColumnMaps(cmap, local)
				mu.Unlock()
				return nil
			}

			pksQID, err := runShow(snowflakeShowInSchemaStmt("SHOW PRIMARY KEYS IN SCHEMA", schema))
			if err != nil {
				return fmt.Errorf("snowflake SHOW PRIMARY KEYS: %w", err)
			}
			uksQID, err := runShow(snowflakeShowInSchemaStmt("SHOW UNIQUE KEYS IN SCHEMA", schema))
			if err != nil {
				return fmt.Errorf("snowflake SHOW UNIQUE KEYS: %w", err)
			}
			fksQID, err := runShow(snowflakeShowInSchemaStmt("SHOW IMPORTED KEYS IN SCHEMA", schema))
			if err != nil {
				return fmt.Errorf("snowflake SHOW IMPORTED KEYS: %w", err)
			}

			qctx, cancel := context.WithTimeout(gctx, introspectionQueryTimeout)
			start := time.Now()
			rows, err := conn.QueryContext(qctx, snowflakeColumnsShowStmt, colsQID, pksQID, uksQID, fksQID)
			recordDiscoveryQuery(ctx, "schema_metadata", snowflakeColumnsShowStmt, time.Since(start))
			if err != nil {
				cancel()
				return err
			}
			if err := scanDiscoveredColumnRows(rows, "snowflake", blockList, local); err != nil {
				cancel()
				return err
			}
			cancel()

			mu.Lock()
			mergeDiscoveredColumnMaps(cmap, local)
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return enrichAndCollectColumns(ctx, db, "snowflake", cmap), nil
}

func discoverSnowflakeUserSchemas(ctx context.Context, db *sql.DB) ([]string, error) {
	const stmt = `
SELECT DISTINCT table_schema
FROM information_schema.tables
WHERE UPPER(table_schema) NOT IN ('INFORMATION_SCHEMA')
ORDER BY table_schema`

	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()
	start := time.Now()
	rows, err := db.QueryContext(qctx, stmt)
	recordDiscoveryQuery(ctx, "schema_metadata", stmt, time.Since(start))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var schema string
		if err := rows.Scan(&schema); err != nil {
			return nil, err
		}
		if strings.TrimSpace(schema) != "" {
			schemas = append(schemas, schema)
		}
	}
	return schemas, rows.Err()
}

func snowflakeShowInSchemaStmt(prefix, schema string) string {
	schema = strings.TrimSpace(schema)
	if schema == "" {
		return prefix
	}
	return prefix + " " + snowflakeQuoteIdent(schema)
}

func snowflakeQuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// discoverClusteringKeys queries Snowflake's information_schema.tables for
// clustering key metadata. Returns a map of "schema:table" → []column_name.
func discoverClusteringKeys(ctx context.Context, db *sql.DB) (map[string][]string, error) {
	qctx, cancel := context.WithTimeout(ctx, introspectionQueryTimeout)
	defer cancel()

	start := time.Now()
	rows, err := db.QueryContext(qctx, snowflakeClusteringStmt)
	recordDiscoveryQuery(ctx, "schema_metadata", snowflakeClusteringStmt, time.Since(start))
	if err != nil {
		return nil, fmt.Errorf("error fetching clustering keys: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]string)
	for rows.Next() {
		var schema, table, clusterExpr string
		if err := rows.Scan(&schema, &table, &clusterExpr); err != nil {
			return nil, fmt.Errorf("error scanning clustering key row: %w", err)
		}
		if keys := ParseClusteringKey(clusterExpr); len(keys) > 0 {
			result[schema+":"+table] = keys
		}
	}
	return result, rows.Err()
}

// autoSetPartitionFromClustering checks if the leading clustering key column
// is a temporal type (date, timestamp, etc.) and, if so, sets it as the
// table's partition key with a default 90-day range filter. This enables
// zero-config partition pruning for Snowflake tables clustered by a date
// column — queries without an explicit filter on the clustering key get a
// `WHERE created_at >= NOW() - 90 days` predicate automatically.
//
// The 60-day default is conservative enough for most workloads while still
// preventing accidental full-table scans. Users can override via config
// (setting PartitionRangeDays to a different value or 0 for warn-only).
func autoSetPartitionFromClustering(t *DBTable) {
	if len(t.ClusteringKeys) == 0 {
		return
	}
	leadingKey := t.ClusteringKeys[0]
	cid, ok := t.GetColumnIndex(leadingKey)
	if !ok {
		return
	}
	if isTemporalType(t.Columns[cid].Type) {
		t.PartitionKey = leadingKey
		t.PartitionRangeDays = 60
	}
}

var implicitPartitionCandidates = []string{
	"created_at",
	"event_time",
	"updated_at",
	"timestamp",
	"ingested_at",
}

func resolveImplicitPartitionKey(t *DBTable) string {
	for _, cand := range implicitPartitionCandidates {
		for i := range t.Columns {
			if strings.EqualFold(t.Columns[i].Name, cand) && isTemporalType(t.Columns[i].Type) {
				return t.Columns[i].Name
			}
		}
	}
	return ""
}

// isTemporalType returns true if the column type string represents a
// date or timestamp type across common database dialects.
func isTemporalType(colType string) bool {
	t := strings.ToLower(colType)
	switch {
	case strings.Contains(t, "timestamp"):
		return true
	case strings.Contains(t, "datetime"):
		return true
	case t == "date":
		return true
	case strings.HasPrefix(t, "timestamp_"):
		// Snowflake: TIMESTAMP_LTZ, TIMESTAMP_NTZ, TIMESTAMP_TZ
		return true
	default:
		return false
	}
}

// ParseClusteringKey parses Snowflake's clustering key expression into
// a list of normalized column names. Snowflake returns expressions like:
//
//	LINEAR(CREATED_AT, USER_ID)
//	LINEAR(CREATED_AT)
//	(CREATED_AT, USER_ID)
//
// Returns nil for empty or unparseable expressions.
func ParseClusteringKey(expr string) []string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil
	}

	// Strip optional LINEAR(...) wrapper
	upper := strings.ToUpper(expr)
	if strings.HasPrefix(upper, "LINEAR(") && strings.HasSuffix(expr, ")") {
		expr = expr[7 : len(expr)-1]
	} else if strings.HasPrefix(expr, "(") && strings.HasSuffix(expr, ")") {
		// Strip bare parentheses
		expr = expr[1 : len(expr)-1]
	}

	parts := strings.Split(expr, ",")
	var keys []string
	for _, p := range parts {
		col := strings.TrimSpace(p)
		if col == "" {
			continue
		}
		// Normalize: snake_case first (to split PascalCase), then lowercase.
		// Snowflake typically returns UPPER_CASE identifiers, but this order
		// also handles the unlikely PascalCase edge case correctly.
		// Note: expression-based clustering keys (e.g., CAST(col AS DATE))
		// will not match any column and are gracefully skipped.
		col = strings.ToLower(util.ToSnake(col))
		keys = append(keys, col)
	}
	return keys
}
