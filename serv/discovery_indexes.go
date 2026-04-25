package serv

import (
	"context"
	"database/sql"
	"log"
	"sort"
	"strings"

	core "github.com/dosco/graphjin/core/v3"
)

// loadIndexes runs one dialect-specific catalog query. nil on failure or
// unsupported dialect (mongodb, snowflake) — treat absence as unknown.
func loadIndexes(ctx context.Context, gj *core.GraphJin, database string, schema *core.TableSchema) []IndexInfo {
	db, dbtype, err := gj.DBForDatabase(database)
	if err != nil || db == nil {
		return nil
	}

	qctx, cancel := context.WithTimeout(ctx, rowCountQueryTimeout)
	defer cancel()

	var idx []IndexInfo
	switch strings.ToLower(dbtype) {
	case "postgres", "postgresql", "cockroachdb", "cockroach":
		idx, err = postgresIndexes(qctx, db, schema)
	case "mysql", "mariadb":
		idx, err = mysqlIndexes(qctx, db, namespaceForSingleTier(database, schema.Schema), schema.Name)
	case "sqlite":
		idx, err = sqliteIndexes(qctx, db, schema.Name)
	case "oracle":
		idx, err = oracleIndexes(qctx, db, schema)
	case "mssql":
		idx, err = mssqlIndexes(qctx, db, schema)
	case "snowflake":
		// Clustering keys, not indexes — skip rather than mislead.
		return nil
	case "mongodb":
		return nil
	default:
		return nil
	}
	if err != nil {
		log.Printf("discovery indexes: %s/%s.%s: %v", dbtype, schema.Schema, schema.Name, err)
		return nil
	}
	sort.SliceStable(idx, func(i, j int) bool {
		if idx[i].Primary != idx[j].Primary {
			return idx[i].Primary
		}
		return idx[i].Name < idx[j].Name
	})
	return idx
}

func postgresIndexes(ctx context.Context, db *sql.DB, schema *core.TableSchema) ([]IndexInfo, error) {
	// Correlated subquery preserves indkey ordering via array_position;
	// avoids LATERAL unnest on int2vector (driver-fragile across pg versions).
	const q = `
SELECT i.relname,
       am.amname,
       ix.indisunique::int,
       ix.indisprimary::int,
       array_to_string(
         ARRAY(
           SELECT a.attname
           FROM pg_attribute a
           WHERE a.attrelid = t.oid
             AND a.attnum = ANY(ix.indkey::int2[])
           ORDER BY array_position(ix.indkey::int2[], a.attnum)
         ),
         ','
       )
FROM pg_class t
JOIN pg_namespace n ON n.oid = t.relnamespace
JOIN pg_index ix ON ix.indrelid = t.oid
JOIN pg_class i ON i.oid = ix.indexrelid
JOIN pg_am am ON am.oid = i.relam
WHERE n.nspname = $1 AND t.relname = $2`
	sname := schema.Schema
	if sname == "" {
		sname = "public"
	}
	return scanIndexRows(ctx, db, q, sname, schema.Name)
}

func mysqlIndexes(ctx context.Context, db *sql.DB, schemaName, table string) ([]IndexInfo, error) {
	const q = `
SELECT INDEX_NAME,
       LOWER(INDEX_TYPE),
       MAX(NON_UNIQUE = 0) AS is_unique,
       MAX(INDEX_NAME = 'PRIMARY') AS is_primary,
       GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX SEPARATOR ',')
FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
GROUP BY INDEX_NAME, INDEX_TYPE`
	return scanIndexRows(ctx, db, q, schemaName, table)
}

func sqliteIndexes(ctx context.Context, db *sql.DB, table string) ([]IndexInfo, error) {
	listQ := `SELECT name, "unique", origin FROM pragma_index_list(?) ORDER BY seq`
	rows, err := db.QueryContext(ctx, listQ, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type idxMeta struct {
		name    string
		unique  bool
		primary bool
	}
	var metas []idxMeta
	for rows.Next() {
		var name, origin string
		var uniq int
		if err := rows.Scan(&name, &uniq, &origin); err != nil {
			return nil, err
		}
		metas = append(metas, idxMeta{name: name, unique: uniq != 0, primary: origin == "pk"})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]IndexInfo, 0, len(metas))
	for _, m := range metas {
		colRows, err := db.QueryContext(ctx, `SELECT name FROM pragma_index_info(?) ORDER BY seqno`, m.name)
		if err != nil {
			continue
		}
		var cols []string
		for colRows.Next() {
			var c sql.NullString
			if err := colRows.Scan(&c); err == nil && c.Valid {
				cols = append(cols, c.String)
			}
		}
		colRows.Close()
		if len(cols) == 0 {
			continue
		}
		out = append(out, IndexInfo{
			Name: m.name, Columns: cols,
			Unique: m.unique, Primary: m.primary, Type: "btree",
		})
	}
	return out, nil
}

func oracleIndexes(ctx context.Context, db *sql.DB, schema *core.TableSchema) ([]IndexInfo, error) {
	if schema.Schema == "" {
		return nil, nil
	}
	const q = `
SELECT i.index_name,
       LOWER(i.index_type),
       CASE WHEN i.uniqueness = 'UNIQUE' THEN 1 ELSE 0 END,
       CASE WHEN c.constraint_type = 'P' THEN 1 ELSE 0 END,
       LISTAGG(ic.column_name, ',') WITHIN GROUP (ORDER BY ic.column_position)
FROM all_indexes i
LEFT JOIN all_constraints c
       ON c.owner = i.owner AND c.index_name = i.index_name AND c.constraint_type = 'P'
JOIN all_ind_columns ic
       ON ic.index_owner = i.owner AND ic.index_name = i.index_name
WHERE i.owner = UPPER(:1) AND i.table_name = UPPER(:2)
GROUP BY i.index_name, i.index_type, i.uniqueness, c.constraint_type`
	out, err := scanIndexRows(ctx, db, q, schema.Schema, schema.Name)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Name = strings.ToLower(out[i].Name)
		for j := range out[i].Columns {
			out[i].Columns[j] = strings.ToLower(out[i].Columns[j])
		}
	}
	return out, nil
}

func mssqlIndexes(ctx context.Context, db *sql.DB, schema *core.TableSchema) ([]IndexInfo, error) {
	sname := schema.Schema
	if sname == "" {
		sname = "dbo"
	}
	const q = `
SELECT i.name,
       LOWER(i.type_desc),
       CAST(i.is_unique AS INT),
       CAST(i.is_primary_key AS INT),
       STRING_AGG(c.name, ',') WITHIN GROUP (ORDER BY ic.key_ordinal)
FROM sys.indexes i
JOIN sys.tables t ON t.object_id = i.object_id
JOIN sys.schemas s ON s.schema_id = t.schema_id
JOIN sys.index_columns ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id
JOIN sys.columns c ON c.object_id = i.object_id AND c.column_id = ic.column_id
WHERE s.name = @p1 AND t.name = @p2 AND i.is_hypothetical = 0 AND i.type > 0
GROUP BY i.name, i.type_desc, i.is_unique, i.is_primary_key`
	return scanIndexRows(ctx, db, q, sname, schema.Name)
}

func scanIndexRows(ctx context.Context, db *sql.DB, q string, args ...any) ([]IndexInfo, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []IndexInfo
	for rows.Next() {
		var name, idxType, cols sql.NullString
		var isUnique, isPrimary sql.NullInt64
		if err := rows.Scan(&name, &idxType, &isUnique, &isPrimary, &cols); err != nil {
			return nil, err
		}
		if !name.Valid || !cols.Valid {
			continue
		}
		colList := strings.Split(cols.String, ",")
		for i := range colList {
			colList[i] = strings.TrimSpace(colList[i])
		}
		out = append(out, IndexInfo{
			Name:    name.String,
			Columns: colList,
			Unique:  isUnique.Valid && isUnique.Int64 != 0,
			Primary: isPrimary.Valid && isPrimary.Int64 != 0,
			Type:    idxType.String,
		})
	}
	return out, rows.Err()
}
