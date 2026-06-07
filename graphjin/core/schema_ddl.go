package core

import (
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// DDLDialect defines how to generate DDL for a specific database
type DDLDialect interface {
	Name() string
	QuoteIdentifier(s string) string
	MapType(graphqlType string, notNull bool, primaryKey bool) string
	MapDefault(defaultVal string) string
	CreateTable(table sdata.DBTable) string
	AddColumn(tableName string, col sdata.DBColumn) string
	DropColumn(tableName, colName string) string
	DropTable(tableName string) string
	AddForeignKey(tableName string, col sdata.DBColumn) string
	CreateSearchIndex(tableName string, col sdata.DBColumn) string
	CreateUniqueIndex(tableName string, col sdata.DBColumn) string
	CreateIndex(tableName string, col sdata.DBColumn) string
	AlterClusteringKey(tableName string, keys []string) string
}

// SupportsSchemaDDL reports whether GraphJin can generate executable DDL for dbType.
func SupportsSchemaDDL(dbType string) bool {
	return getDDLDialect(dbType) != nil
}

func getDDLDialect(dbType string) DDLDialect {
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "postgresql", "postgres":
		return &postgresDialect{}
	case "mysql":
		return &mysqlDialect{}
	case "mariadb":
		return &mariadbDialect{}
	case "sqlite":
		return &sqliteDialect{}
	case "mssql":
		return &mssqlDialect{}
	case "oracle":
		return &oracleDialect{}
	case "snowflake":
		return &snowflakeDDLDialect{}
	case "bigquery":
		return &bigqueryDDLDialect{}
	case "redshift":
		return &redshiftDDLDialect{}
	case "cassandra":
		return &cassandraDDLDialect{}
	default:
		return nil
	}
}

// PostgreSQL dialect
type postgresDialect struct{}

func (d *postgresDialect) Name() string { return "postgresql" }

func (d *postgresDialect) QuoteIdentifier(s string) string {
	return `"` + s + `"`
}

func (d *postgresDialect) MapType(graphqlType string, notNull bool, primaryKey bool) string {
	t := strings.ToLower(graphqlType)

	if primaryKey {
		switch t {
		case "int", "integer", "bigint", "big int":
			return "BIGSERIAL PRIMARY KEY"
		case "smallint", "small int":
			return "SMALLSERIAL PRIMARY KEY"
		default:
			return d.mapBaseType(t) + " PRIMARY KEY"
		}
	}

	baseType := d.mapBaseType(t)
	if notNull {
		return baseType + " NOT NULL"
	}
	return baseType
}

func (d *postgresDialect) mapBaseType(t string) string {
	// Handle type aliases with embedded sizes
	if baseType, size := parseTypeWithSize(t); size != "" {
		switch baseType {
		case "varchar":
			return fmt.Sprintf("VARCHAR(%s)", size)
		case "char":
			return fmt.Sprintf("CHAR(%s)", size)
		case "decimal", "numeric":
			return fmt.Sprintf("NUMERIC(%s)", size)
		}
	}

	switch t {
	case "int", "integer":
		return "INTEGER"
	case "bigint", "big int":
		return "BIGINT"
	case "smallint", "small int":
		return "SMALLINT"
	case "float", "real":
		return "REAL"
	case "double", "double precision":
		return "DOUBLE PRECISION"
	case "decimal", "numeric":
		return "NUMERIC"
	case "boolean", "bool":
		return "BOOLEAN"
	case "text", "string":
		return "TEXT"
	case "varchar", "character varying":
		return "VARCHAR(255)"
	case "char", "character":
		return "CHAR(1)"
	case "timestamp", "timestamp with time zone", "timestamptz":
		return "TIMESTAMPTZ"
	case "timestamp without time zone":
		return "TIMESTAMP"
	case "date":
		return "DATE"
	case "time", "time with time zone", "timetz":
		return "TIMETZ"
	case "time without time zone":
		return "TIME"
	case "interval":
		return "INTERVAL"
	case "json":
		return "JSON"
	case "jsonb":
		return "JSONB"
	case "uuid":
		return "UUID"
	case "bytea", "bytes":
		return "BYTEA"
	case "inet":
		return "INET"
	case "cidr":
		return "CIDR"
	case "macaddr":
		return "MACADDR"
	case "point":
		return "POINT"
	case "line":
		return "LINE"
	case "polygon":
		return "POLYGON"
	case "geometry":
		return "GEOMETRY"
	case "geography":
		return "GEOGRAPHY"
	case "money":
		return "MONEY"
	case "xml":
		return "XML"
	case "serial":
		return "SERIAL"
	case "bigserial", "big serial":
		return "BIGSERIAL"
	default:
		return "TEXT"
	}
}

func (d *postgresDialect) MapDefault(defaultVal string) string {
	return defaultVal
}

func (d *postgresDialect) CreateTable(table sdata.DBTable) string {
	var cols []string
	var constraints []string

	for _, col := range table.Columns {
		colDef := fmt.Sprintf("  %s %s",
			d.QuoteIdentifier(col.Name),
			d.MapType(col.Type, col.NotNull, col.PrimaryKey))
		if col.Default != "" {
			colDef += fmt.Sprintf(" DEFAULT %s", d.MapDefault(col.Default))
		}
		cols = append(cols, colDef)

		if col.FKeyTable != "" && col.FKeyCol != "" {
			fkName := fmt.Sprintf("fk_%s_%s", table.Name, col.Name)
			fkDef := fmt.Sprintf("  CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s)",
				d.QuoteIdentifier(fkName),
				d.QuoteIdentifier(col.Name),
				d.QuoteIdentifier(col.FKeyTable),
				d.QuoteIdentifier(col.FKeyCol))
			if col.FKOnDelete != "" {
				fkDef += fmt.Sprintf(" ON DELETE %s", col.FKOnDelete)
			}
			if col.FKOnUpdate != "" {
				fkDef += fmt.Sprintf(" ON UPDATE %s", col.FKOnUpdate)
			}
			constraints = append(constraints, fkDef)
		}
	}

	tableParts := append(cols, constraints...)
	return fmt.Sprintf("CREATE TABLE %s (\n%s\n);",
		d.QuoteIdentifier(table.Name),
		strings.Join(tableParts, ",\n"))
}

func (d *postgresDialect) AddColumn(tableName string, col sdata.DBColumn) string {
	colDef := d.MapType(col.Type, col.NotNull, false)
	if col.Default != "" {
		colDef += fmt.Sprintf(" DEFAULT %s", d.MapDefault(col.Default))
	}
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(col.Name),
		colDef)
}

func (d *postgresDialect) DropColumn(tableName, colName string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(colName))
}

func (d *postgresDialect) DropTable(tableName string) string {
	return fmt.Sprintf("DROP TABLE %s;", d.QuoteIdentifier(tableName))
}

func (d *postgresDialect) AddForeignKey(tableName string, col sdata.DBColumn) string {
	if col.FKeyTable == "" || col.FKeyCol == "" {
		return ""
	}
	fkName := fmt.Sprintf("fk_%s_%s", tableName, col.Name)
	sql := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s)",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(fkName),
		d.QuoteIdentifier(col.Name),
		d.QuoteIdentifier(col.FKeyTable),
		d.QuoteIdentifier(col.FKeyCol))
	if col.FKOnDelete != "" {
		sql += fmt.Sprintf(" ON DELETE %s", col.FKOnDelete)
	}
	if col.FKOnUpdate != "" {
		sql += fmt.Sprintf(" ON UPDATE %s", col.FKOnUpdate)
	}
	return sql + ";"
}

func (d *postgresDialect) CreateSearchIndex(tableName string, col sdata.DBColumn) string {
	idxName := fmt.Sprintf("idx_%s_%s_search", tableName, col.Name)
	return fmt.Sprintf("CREATE INDEX %s ON %s USING gin(to_tsvector('english', %s));",
		d.QuoteIdentifier(idxName),
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(col.Name))
}

func (d *postgresDialect) CreateUniqueIndex(tableName string, col sdata.DBColumn) string {
	idxName := fmt.Sprintf("idx_%s_%s_unique", tableName, col.Name)
	return fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s);",
		d.QuoteIdentifier(idxName),
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(col.Name))
}

func (d *postgresDialect) CreateIndex(tableName string, col sdata.DBColumn) string {
	idxName := col.IndexName
	if idxName == "" {
		idxName = fmt.Sprintf("idx_%s_%s", tableName, col.Name)
	}
	return fmt.Sprintf("CREATE INDEX %s ON %s (%s);",
		d.QuoteIdentifier(idxName),
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(col.Name))
}

func (d *postgresDialect) AlterClusteringKey(_ string, _ []string) string {
	return ""
}

// MySQL dialect
type mysqlDialect struct{}

func (d *mysqlDialect) Name() string { return "mysql" }

func (d *mysqlDialect) QuoteIdentifier(s string) string {
	return "`" + s + "`"
}

func (d *mysqlDialect) MapType(graphqlType string, notNull bool, primaryKey bool) string {
	t := strings.ToLower(graphqlType)

	if primaryKey {
		switch t {
		case "int", "integer", "bigint", "big int":
			return "BIGINT AUTO_INCREMENT PRIMARY KEY"
		case "smallint", "small int":
			return "INT AUTO_INCREMENT PRIMARY KEY"
		default:
			return d.mapBaseType(t) + " PRIMARY KEY"
		}
	}

	baseType := d.mapBaseType(t)
	if notNull {
		return baseType + " NOT NULL"
	}
	return baseType
}

func (d *mysqlDialect) mapBaseType(t string) string {
	// Handle type aliases with embedded sizes
	if baseType, size := parseTypeWithSize(t); size != "" {
		switch baseType {
		case "varchar":
			return fmt.Sprintf("VARCHAR(%s)", size)
		case "char":
			return fmt.Sprintf("CHAR(%s)", size)
		case "decimal", "numeric":
			return fmt.Sprintf("DECIMAL(%s)", size)
		}
	}

	switch t {
	case "int", "integer":
		return "INT"
	case "bigint", "big int":
		return "BIGINT"
	case "smallint", "small int":
		return "SMALLINT"
	case "float", "real":
		return "FLOAT"
	case "double", "double precision":
		return "DOUBLE"
	case "decimal", "numeric":
		return "DECIMAL(10,2)"
	case "boolean", "bool":
		return "TINYINT(1)"
	case "text", "string":
		return "TEXT"
	case "varchar", "character varying":
		return "VARCHAR(255)"
	case "char", "character":
		return "CHAR(1)"
	case "timestamp", "timestamp with time zone", "timestamptz":
		return "DATETIME"
	case "timestamp without time zone":
		return "DATETIME"
	case "date":
		return "DATE"
	case "time", "time with time zone", "timetz":
		return "TIME"
	case "time without time zone":
		return "TIME"
	case "interval":
		return "VARCHAR(255)"
	case "json", "jsonb":
		return "JSON"
	case "uuid":
		return "CHAR(36)"
	case "bytea", "bytes":
		return "BLOB"
	case "point":
		return "POINT"
	case "polygon":
		return "POLYGON"
	case "geometry":
		return "GEOMETRY"
	case "money":
		return "DECIMAL(19,4)"
	case "xml":
		return "LONGTEXT"
	case "serial":
		return "INT AUTO_INCREMENT"
	case "bigserial", "big serial":
		return "BIGINT AUTO_INCREMENT"
	default:
		return "TEXT"
	}
}

func (d *mysqlDialect) MapDefault(defaultVal string) string {
	return defaultVal
}

func (d *mysqlDialect) CreateTable(table sdata.DBTable) string {
	var cols []string
	var constraints []string

	for _, col := range table.Columns {
		colDef := fmt.Sprintf("  %s %s",
			d.QuoteIdentifier(col.Name),
			d.MapType(col.Type, col.NotNull, col.PrimaryKey))
		if col.Default != "" {
			colDef += fmt.Sprintf(" DEFAULT %s", d.MapDefault(col.Default))
		}
		cols = append(cols, colDef)

		if col.FKeyTable != "" && col.FKeyCol != "" {
			fkName := fmt.Sprintf("fk_%s_%s", table.Name, col.Name)
			fkDef := fmt.Sprintf("  CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s)",
				d.QuoteIdentifier(fkName),
				d.QuoteIdentifier(col.Name),
				d.QuoteIdentifier(col.FKeyTable),
				d.QuoteIdentifier(col.FKeyCol))
			if col.FKOnDelete != "" {
				fkDef += fmt.Sprintf(" ON DELETE %s", col.FKOnDelete)
			}
			if col.FKOnUpdate != "" {
				fkDef += fmt.Sprintf(" ON UPDATE %s", col.FKOnUpdate)
			}
			constraints = append(constraints, fkDef)
		}
	}

	tableParts := append(cols, constraints...)
	return fmt.Sprintf("CREATE TABLE %s (\n%s\n) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;",
		d.QuoteIdentifier(table.Name),
		strings.Join(tableParts, ",\n"))
}

func (d *mysqlDialect) AddColumn(tableName string, col sdata.DBColumn) string {
	colDef := d.MapType(col.Type, col.NotNull, false)
	if col.Default != "" {
		colDef += fmt.Sprintf(" DEFAULT %s", d.MapDefault(col.Default))
	}
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(col.Name),
		colDef)
}

func (d *mysqlDialect) DropColumn(tableName, colName string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(colName))
}

func (d *mysqlDialect) DropTable(tableName string) string {
	return fmt.Sprintf("DROP TABLE %s;", d.QuoteIdentifier(tableName))
}

func (d *mysqlDialect) AddForeignKey(tableName string, col sdata.DBColumn) string {
	if col.FKeyTable == "" || col.FKeyCol == "" {
		return ""
	}
	fkName := fmt.Sprintf("fk_%s_%s", tableName, col.Name)
	sql := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s)",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(fkName),
		d.QuoteIdentifier(col.Name),
		d.QuoteIdentifier(col.FKeyTable),
		d.QuoteIdentifier(col.FKeyCol))
	if col.FKOnDelete != "" {
		sql += fmt.Sprintf(" ON DELETE %s", col.FKOnDelete)
	}
	if col.FKOnUpdate != "" {
		sql += fmt.Sprintf(" ON UPDATE %s", col.FKOnUpdate)
	}
	return sql + ";"
}

func (d *mysqlDialect) CreateSearchIndex(tableName string, col sdata.DBColumn) string {
	idxName := fmt.Sprintf("idx_%s_%s_fulltext", tableName, col.Name)
	return fmt.Sprintf("CREATE FULLTEXT INDEX %s ON %s (%s);",
		d.QuoteIdentifier(idxName),
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(col.Name))
}

func (d *mysqlDialect) CreateUniqueIndex(tableName string, col sdata.DBColumn) string {
	idxName := fmt.Sprintf("idx_%s_%s_unique", tableName, col.Name)
	return fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s);",
		d.QuoteIdentifier(idxName),
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(col.Name))
}

func (d *mysqlDialect) CreateIndex(tableName string, col sdata.DBColumn) string {
	idxName := col.IndexName
	if idxName == "" {
		idxName = fmt.Sprintf("idx_%s_%s", tableName, col.Name)
	}
	return fmt.Sprintf("CREATE INDEX %s ON %s (%s);",
		d.QuoteIdentifier(idxName),
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(col.Name))
}

func (d *mysqlDialect) AlterClusteringKey(_ string, _ []string) string {
	return ""
}

// MariaDB dialect (extends MySQL)
type mariadbDialect struct {
	mysqlDialect
}

func (d *mariadbDialect) Name() string { return "mariadb" }

// Snowflake dialect
type snowflakeDDLDialect struct{}

func (d *snowflakeDDLDialect) Name() string { return "snowflake" }

func (d *snowflakeDDLDialect) QuoteIdentifier(s string) string {
	return `"` + s + `"`
}

func (d *snowflakeDDLDialect) MapType(graphqlType string, notNull bool, primaryKey bool) string {
	t := strings.ToLower(graphqlType)

	if primaryKey {
		switch t {
		case "int", "integer":
			return "INTEGER NOT NULL PRIMARY KEY"
		case "bigint", "big int":
			return "BIGINT NOT NULL PRIMARY KEY"
		case "smallint", "small int":
			return "SMALLINT NOT NULL PRIMARY KEY"
		default:
			return d.mapBaseType(t) + " NOT NULL PRIMARY KEY"
		}
	}

	baseType := d.mapBaseType(t)
	if notNull {
		return baseType + " NOT NULL"
	}
	return baseType
}

func (d *snowflakeDDLDialect) mapBaseType(t string) string {
	// Handle type aliases with embedded sizes
	if baseType, size := parseTypeWithSize(t); size != "" {
		switch baseType {
		case "varchar":
			return fmt.Sprintf("VARCHAR(%s)", size)
		case "char":
			return fmt.Sprintf("CHAR(%s)", size)
		case "decimal", "numeric":
			return fmt.Sprintf("NUMBER(%s)", size)
		}
	}

	switch t {
	case "int", "integer":
		return "INTEGER"
	case "bigint", "big int":
		return "BIGINT"
	case "smallint", "small int":
		return "SMALLINT"
	case "float", "real":
		return "FLOAT"
	case "double", "double precision":
		return "DOUBLE"
	case "decimal", "numeric":
		return "NUMBER(10,2)"
	case "boolean", "bool":
		return "BOOLEAN"
	case "text", "string":
		return "VARCHAR"
	case "varchar", "character varying":
		return "VARCHAR(255)"
	case "char", "character":
		return "CHAR(1)"
	case "timestamp", "timestamp with time zone", "timestamptz", "timestamp without time zone":
		return "TIMESTAMP"
	case "date":
		return "DATE"
	case "time", "time with time zone", "timetz", "time without time zone":
		return "TIME"
	case "interval":
		return "VARCHAR"
	case "json", "jsonb":
		return "JSON"
	case "uuid":
		return "VARCHAR(36)"
	case "bytea", "bytes":
		return "BINARY"
	case "money":
		return "NUMBER(19,4)"
	case "xml":
		return "VARCHAR"
	case "serial", "bigserial", "big serial":
		return "BIGINT"
	default:
		return "VARCHAR"
	}
}

func (d *snowflakeDDLDialect) MapDefault(defaultVal string) string {
	return defaultVal
}

func (d *snowflakeDDLDialect) CreateTable(table sdata.DBTable) string {
	var cols []string
	var constraints []string

	for _, col := range table.Columns {
		colDef := fmt.Sprintf("  %s %s",
			d.QuoteIdentifier(col.Name),
			d.MapType(col.Type, col.NotNull, col.PrimaryKey))
		if col.Default != "" {
			colDef += fmt.Sprintf(" DEFAULT %s", d.MapDefault(col.Default))
		}
		cols = append(cols, colDef)

		if col.FKeyTable != "" && col.FKeyCol != "" {
			fkName := fmt.Sprintf("fk_%s_%s", table.Name, col.Name)
			fkDef := fmt.Sprintf("  CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s)",
				d.QuoteIdentifier(fkName),
				d.QuoteIdentifier(col.Name),
				d.QuoteIdentifier(col.FKeyTable),
				d.QuoteIdentifier(col.FKeyCol))
			if col.FKOnDelete != "" {
				fkDef += fmt.Sprintf(" ON DELETE %s", col.FKOnDelete)
			}
			if col.FKOnUpdate != "" {
				fkDef += fmt.Sprintf(" ON UPDATE %s", col.FKOnUpdate)
			}
			constraints = append(constraints, fkDef)
		}
	}

	tableParts := append(cols, constraints...)
	sql := fmt.Sprintf("CREATE TABLE %s (\n%s\n)",
		d.QuoteIdentifier(table.Name),
		strings.Join(tableParts, ",\n"))

	if len(table.ClusteringKeys) > 0 {
		quoted := make([]string, len(table.ClusteringKeys))
		for i, k := range table.ClusteringKeys {
			quoted[i] = d.QuoteIdentifier(k)
		}
		sql += fmt.Sprintf(" CLUSTER BY (%s)", strings.Join(quoted, ", "))
	}

	return sql + ";"
}

func (d *snowflakeDDLDialect) AddColumn(tableName string, col sdata.DBColumn) string {
	colDef := d.MapType(col.Type, col.NotNull, false)
	if col.Default != "" {
		colDef += fmt.Sprintf(" DEFAULT %s", d.MapDefault(col.Default))
	}
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(col.Name),
		colDef)
}

func (d *snowflakeDDLDialect) DropColumn(tableName, colName string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(colName))
}

func (d *snowflakeDDLDialect) DropTable(tableName string) string {
	return fmt.Sprintf("DROP TABLE %s;", d.QuoteIdentifier(tableName))
}

func (d *snowflakeDDLDialect) AddForeignKey(tableName string, col sdata.DBColumn) string {
	if col.FKeyTable == "" || col.FKeyCol == "" {
		return ""
	}
	fkName := fmt.Sprintf("fk_%s_%s", tableName, col.Name)
	sql := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s)",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(fkName),
		d.QuoteIdentifier(col.Name),
		d.QuoteIdentifier(col.FKeyTable),
		d.QuoteIdentifier(col.FKeyCol))
	if col.FKOnDelete != "" {
		sql += fmt.Sprintf(" ON DELETE %s", col.FKOnDelete)
	}
	if col.FKOnUpdate != "" {
		sql += fmt.Sprintf(" ON UPDATE %s", col.FKOnUpdate)
	}
	return sql + ";"
}

func (d *snowflakeDDLDialect) CreateSearchIndex(_ string, _ sdata.DBColumn) string {
	// Snowflake uses Search Optimization Service instead of CREATE INDEX-based FTS.
	return ""
}

func (d *snowflakeDDLDialect) CreateUniqueIndex(tableName string, col sdata.DBColumn) string {
	idxName := fmt.Sprintf("idx_%s_%s_unique", tableName, col.Name)
	return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s UNIQUE (%s);",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(idxName),
		d.QuoteIdentifier(col.Name))
}

func (d *snowflakeDDLDialect) CreateIndex(_ string, _ sdata.DBColumn) string {
	// Snowflake has no user-managed B-tree indexes.
	return ""
}

func (d *snowflakeDDLDialect) AlterClusteringKey(tableName string, keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	quoted := make([]string, len(keys))
	for i, k := range keys {
		quoted[i] = d.QuoteIdentifier(k)
	}
	return fmt.Sprintf("ALTER TABLE %s CLUSTER BY (%s);",
		d.QuoteIdentifier(tableName),
		strings.Join(quoted, ", "))
}

type bigqueryDDLDialect struct{}

func (d *bigqueryDDLDialect) Name() string { return "bigquery" }

func (d *bigqueryDDLDialect) QuoteIdentifier(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "\\`") + "`"
}

func (d *bigqueryDDLDialect) MapType(graphqlType string, notNull bool, primaryKey bool) string {
	t := strings.ToLower(graphqlType)
	baseType := d.mapBaseType(t)
	if primaryKey {
		return baseType + " NOT NULL PRIMARY KEY NOT ENFORCED"
	}
	if notNull {
		return baseType + " NOT NULL"
	}
	return baseType
}

func (d *bigqueryDDLDialect) mapColumnType(col sdata.DBColumn, primaryKey bool) string {
	if col.Array {
		base := d.mapBaseType(strings.ToLower(col.Type))
		if strings.HasPrefix(base, "ARRAY<") {
			base = strings.TrimPrefix(strings.TrimSuffix(base, ">"), "ARRAY<")
		}
		out := fmt.Sprintf("ARRAY<%s>", base)
		if col.NotNull {
			out += " NOT NULL"
		}
		return out
	}
	return d.MapType(col.Type, col.NotNull, primaryKey)
}

func (d *bigqueryDDLDialect) mapBaseType(t string) string {
	if baseType, _ := parseTypeWithSize(t); baseType != "" {
		switch baseType {
		case "decimal", "numeric":
			return "NUMERIC"
		default:
			return "STRING"
		}
	}

	switch t {
	case "int", "integer", "bigint", "big int", "smallint", "small int", "serial", "bigserial", "big serial":
		return "INT64"
	case "float", "real", "double", "double precision":
		return "FLOAT64"
	case "decimal", "numeric", "money":
		return "NUMERIC"
	case "boolean", "bool":
		return "BOOL"
	case "text", "string", "varchar", "character varying", "char", "character", "uuid", "xml", "interval":
		return "STRING"
	case "timestamp", "timestamp with time zone", "timestamptz", "timestamp without time zone":
		return "TIMESTAMP"
	case "date":
		return "DATE"
	case "time", "time with time zone", "timetz", "time without time zone":
		return "TIME"
	case "json", "jsonb":
		return "JSON"
	case "bytea", "bytes", "varbyte":
		return "BYTES"
	case "geography", "geometry":
		return "GEOGRAPHY"
	default:
		return "STRING"
	}
}

func (d *bigqueryDDLDialect) MapDefault(defaultVal string) string {
	return defaultVal
}

func (d *bigqueryDDLDialect) CreateTable(table sdata.DBTable) string {
	var cols []string
	var constraints []string
	primaryCols := table.PKColNames()
	compositePK := len(primaryCols) > 1

	for _, col := range table.Columns {
		colDef := fmt.Sprintf("  %s %s",
			d.QuoteIdentifier(col.Name),
			d.mapColumnType(col, col.PrimaryKey && !compositePK))
		if col.Default != "" {
			colDef += fmt.Sprintf(" DEFAULT %s", d.MapDefault(col.Default))
		}
		if col.FKeyTable != "" && col.FKeyCol != "" {
			colDef += fmt.Sprintf(" REFERENCES %s(%s) NOT ENFORCED",
				d.tableRef(col.FKeySchema, col.FKeyTable),
				d.QuoteIdentifier(col.FKeyCol))
		}
		cols = append(cols, colDef)
	}

	if compositePK {
		quoted := make([]string, len(primaryCols))
		for i, col := range primaryCols {
			quoted[i] = d.QuoteIdentifier(col)
		}
		constraints = append(constraints,
			fmt.Sprintf("  PRIMARY KEY (%s) NOT ENFORCED", strings.Join(quoted, ", ")))
	}

	tableParts := append(cols, constraints...)
	sql := fmt.Sprintf("CREATE TABLE %s (\n%s\n)",
		d.tableRef(table.Schema, table.Name),
		strings.Join(tableParts, ",\n"))

	if len(table.ClusteringKeys) > 0 {
		quoted := make([]string, len(table.ClusteringKeys))
		for i, k := range table.ClusteringKeys {
			quoted[i] = d.QuoteIdentifier(k)
		}
		sql += fmt.Sprintf("\nCLUSTER BY %s", strings.Join(quoted, ", "))
	}

	return sql + ";"
}

func (d *bigqueryDDLDialect) AddColumn(tableName string, col sdata.DBColumn) string {
	colDef := d.mapColumnType(col, false)
	if col.Default != "" {
		colDef += fmt.Sprintf(" DEFAULT %s", d.MapDefault(col.Default))
	}
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;",
		d.tableRef(col.Schema, tableName),
		d.QuoteIdentifier(col.Name),
		colDef)
}

func (d *bigqueryDDLDialect) DropColumn(tableName, colName string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;",
		d.tableRef("", tableName),
		d.QuoteIdentifier(colName))
}

func (d *bigqueryDDLDialect) DropTable(tableName string) string {
	return fmt.Sprintf("DROP TABLE %s;", d.tableRef("", tableName))
}

func (d *bigqueryDDLDialect) AddForeignKey(tableName string, col sdata.DBColumn) string {
	if col.FKeyTable == "" || col.FKeyCol == "" {
		return ""
	}
	fkName := fmt.Sprintf("fk_%s_%s", tableName, col.Name)
	return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s) NOT ENFORCED;",
		d.tableRef(col.Schema, tableName),
		d.QuoteIdentifier(fkName),
		d.QuoteIdentifier(col.Name),
		d.tableRef(col.FKeySchema, col.FKeyTable),
		d.QuoteIdentifier(col.FKeyCol))
}

func (d *bigqueryDDLDialect) CreateSearchIndex(_ string, _ sdata.DBColumn) string {
	return ""
}

func (d *bigqueryDDLDialect) CreateUniqueIndex(_ string, _ sdata.DBColumn) string {
	return ""
}

func (d *bigqueryDDLDialect) CreateIndex(_ string, _ sdata.DBColumn) string {
	return ""
}

func (d *bigqueryDDLDialect) AlterClusteringKey(_ string, _ []string) string {
	return ""
}

func (d *bigqueryDDLDialect) tableRef(schema, table string) string {
	schema = strings.Trim(strings.TrimSpace(schema), "`")
	table = strings.Trim(strings.TrimSpace(table), "`")
	if schema != "" {
		return d.QuoteIdentifier(schema + "." + table)
	}
	return d.QuoteIdentifier(table)
}

type redshiftDDLDialect struct{}

func (d *redshiftDDLDialect) Name() string { return "redshift" }

func (d *redshiftDDLDialect) QuoteIdentifier(s string) string {
	return `"` + s + `"`
}

func (d *redshiftDDLDialect) MapType(graphqlType string, notNull bool, primaryKey bool) string {
	t := strings.ToLower(graphqlType)
	baseType := d.mapBaseType(t)
	if primaryKey {
		switch t {
		case "int", "integer", "bigint", "big int":
			return baseType + " IDENTITY(1,1) PRIMARY KEY"
		case "serial", "bigserial", "big serial":
			return baseType + " PRIMARY KEY"
		}
		return baseType + " NOT NULL PRIMARY KEY"
	}
	if notNull {
		return baseType + " NOT NULL"
	}
	return baseType
}

func (d *redshiftDDLDialect) mapBaseType(t string) string {
	if baseType, size := parseTypeWithSize(t); size != "" {
		switch baseType {
		case "varchar", "character varying":
			return fmt.Sprintf("VARCHAR(%s)", size)
		case "char", "character":
			return fmt.Sprintf("CHAR(%s)", size)
		case "decimal", "numeric":
			return fmt.Sprintf("DECIMAL(%s)", size)
		}
	}

	switch t {
	case "int", "integer":
		return "INTEGER"
	case "bigint", "big int":
		return "BIGINT"
	case "smallint", "small int":
		return "SMALLINT"
	case "float", "real":
		return "REAL"
	case "double", "double precision":
		return "DOUBLE PRECISION"
	case "decimal", "numeric":
		return "DECIMAL(10,2)"
	case "boolean", "bool":
		return "BOOLEAN"
	case "text", "string":
		return "VARCHAR"
	case "varchar", "character varying":
		return "VARCHAR(255)"
	case "char", "character":
		return "CHAR(1)"
	case "timestamp", "timestamp without time zone":
		return "TIMESTAMP"
	case "timestamp with time zone", "timestamptz":
		return "TIMESTAMPTZ"
	case "date":
		return "DATE"
	case "time", "time without time zone":
		return "TIME"
	case "time with time zone", "timetz":
		return "TIMETZ"
	case "interval":
		return "INTERVAL"
	case "json", "jsonb", "super":
		return "SUPER"
	case "geometry":
		return "GEOMETRY"
	case "geography":
		return "GEOGRAPHY"
	case "hllsketch":
		return "HLLSKETCH"
	case "uuid":
		return "VARCHAR(36)"
	case "bytea", "bytes", "varbyte":
		return "VARBYTE"
	case "money":
		return "DECIMAL(19,4)"
	case "xml":
		return "VARCHAR"
	case "serial":
		return "INT IDENTITY(1,1)"
	case "bigserial", "big serial":
		return "BIGINT IDENTITY(1,1)"
	default:
		return "VARCHAR"
	}
}

func (d *redshiftDDLDialect) MapDefault(defaultVal string) string {
	return defaultVal
}

func (d *redshiftDDLDialect) CreateTable(table sdata.DBTable) string {
	var cols []string
	var constraints []string
	primaryCols := table.PKColNames()
	compositePK := len(primaryCols) > 1

	for _, col := range table.Columns {
		colDef := fmt.Sprintf("  %s %s",
			d.QuoteIdentifier(col.Name),
			d.MapType(col.Type, col.NotNull || (col.PrimaryKey && compositePK), col.PrimaryKey && !compositePK))
		if col.Default != "" {
			colDef += fmt.Sprintf(" DEFAULT %s", d.MapDefault(col.Default))
		}
		cols = append(cols, colDef)

		if col.FKeyTable != "" && col.FKeyCol != "" {
			fkName := fmt.Sprintf("fk_%s_%s", table.Name, col.Name)
			fkDef := fmt.Sprintf("  CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s)",
				d.QuoteIdentifier(fkName),
				d.QuoteIdentifier(col.Name),
				d.QuoteIdentifier(col.FKeyTable),
				d.QuoteIdentifier(col.FKeyCol))
			constraints = append(constraints, fkDef)
		}
	}
	if compositePK {
		quoted := make([]string, len(primaryCols))
		for i, col := range primaryCols {
			quoted[i] = d.QuoteIdentifier(col)
		}
		constraints = append(constraints, fmt.Sprintf("  PRIMARY KEY (%s)", strings.Join(quoted, ", ")))
	}

	tableParts := append(cols, constraints...)
	sql := fmt.Sprintf("CREATE TABLE %s (\n%s\n) DISTSTYLE AUTO ENCODE AUTO",
		d.QuoteIdentifier(table.Name),
		strings.Join(tableParts, ",\n"))

	if len(table.ClusteringKeys) > 0 {
		quoted := make([]string, len(table.ClusteringKeys))
		for i, k := range table.ClusteringKeys {
			quoted[i] = d.QuoteIdentifier(k)
		}
		sql += fmt.Sprintf(" COMPOUND SORTKEY(%s)", strings.Join(quoted, ", "))
	}

	return sql + ";"
}

func (d *redshiftDDLDialect) AddColumn(tableName string, col sdata.DBColumn) string {
	colDef := d.MapType(col.Type, col.NotNull, false)
	if col.Default != "" {
		colDef += fmt.Sprintf(" DEFAULT %s", d.MapDefault(col.Default))
	}
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(col.Name),
		colDef)
}

func (d *redshiftDDLDialect) DropColumn(tableName, colName string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(colName))
}

func (d *redshiftDDLDialect) DropTable(tableName string) string {
	return fmt.Sprintf("DROP TABLE %s;", d.QuoteIdentifier(tableName))
}

func (d *redshiftDDLDialect) AddForeignKey(tableName string, col sdata.DBColumn) string {
	if col.FKeyTable == "" || col.FKeyCol == "" {
		return ""
	}
	fkName := fmt.Sprintf("fk_%s_%s", tableName, col.Name)
	return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s);",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(fkName),
		d.QuoteIdentifier(col.Name),
		d.QuoteIdentifier(col.FKeyTable),
		d.QuoteIdentifier(col.FKeyCol))
}

func (d *redshiftDDLDialect) CreateSearchIndex(_ string, _ sdata.DBColumn) string {
	return ""
}

func (d *redshiftDDLDialect) CreateUniqueIndex(_ string, _ sdata.DBColumn) string {
	return ""
}

func (d *redshiftDDLDialect) CreateIndex(_ string, _ sdata.DBColumn) string {
	return ""
}

func (d *redshiftDDLDialect) AlterClusteringKey(tableName string, keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	quoted := make([]string, len(keys))
	for i, k := range keys {
		quoted[i] = d.QuoteIdentifier(k)
	}
	return fmt.Sprintf("ALTER TABLE %s ALTER SORTKEY(%s);",
		d.QuoteIdentifier(tableName),
		strings.Join(quoted, ", "))
}

type cassandraDDLDialect struct{}

func (d *cassandraDDLDialect) Name() string { return "cassandra" }

func (d *cassandraDDLDialect) QuoteIdentifier(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func (d *cassandraDDLDialect) MapType(graphqlType string, _ bool, _ bool) string {
	return d.mapBaseType(strings.ToLower(graphqlType))
}

func (d *cassandraDDLDialect) mapColumnType(col sdata.DBColumn) string {
	baseType := d.mapBaseType(strings.ToLower(col.Type))
	if col.Array {
		return "list<" + baseType + ">"
	}
	return baseType
}

func (d *cassandraDDLDialect) mapBaseType(t string) string {
	if baseType, _ := parseTypeWithSize(t); baseType != "" {
		switch baseType {
		case "decimal", "numeric":
			return "decimal"
		default:
			return "text"
		}
	}

	switch t {
	case "int", "integer":
		return "int"
	case "bigint", "big int", "serial", "bigserial", "big serial":
		return "bigint"
	case "smallint", "small int":
		return "smallint"
	case "float", "real":
		return "float"
	case "double", "double precision":
		return "double"
	case "decimal", "numeric", "money":
		return "decimal"
	case "boolean", "bool":
		return "boolean"
	case "timestamp", "timestamp with time zone", "timestamptz", "timestamp without time zone":
		return "timestamp"
	case "date":
		return "date"
	case "time", "time with time zone", "timetz", "time without time zone":
		return "time"
	case "uuid":
		return "uuid"
	case "bytea", "bytes", "varbyte":
		return "blob"
	case "json", "jsonb", "text", "string", "varchar", "character varying", "char", "character", "interval", "xml":
		return "text"
	default:
		return "text"
	}
}

func (d *cassandraDDLDialect) MapDefault(defaultVal string) string {
	return defaultVal
}

func (d *cassandraDDLDialect) CreateTable(table sdata.DBTable) string {
	var cols []string
	primaryCols := table.PKColNames()
	if len(primaryCols) == 0 {
		for _, col := range table.Columns {
			if col.PrimaryKey {
				primaryCols = append(primaryCols, col.Name)
			}
		}
	}
	inlinePK := len(primaryCols) == 1

	for _, col := range table.Columns {
		colDef := fmt.Sprintf("  %s %s", d.QuoteIdentifier(col.Name), d.mapColumnType(col))
		if inlinePK && col.PrimaryKey {
			colDef += " PRIMARY KEY"
		}
		cols = append(cols, colDef)
	}
	if len(primaryCols) > 1 {
		quoted := make([]string, len(primaryCols))
		for i, col := range primaryCols {
			quoted[i] = d.QuoteIdentifier(col)
		}
		cols = append(cols, fmt.Sprintf("  PRIMARY KEY ((%s), %s)", quoted[0], strings.Join(quoted[1:], ", ")))
	}

	return fmt.Sprintf("CREATE TABLE %s (\n%s\n);",
		cassandraQualifiedTableName(table.Schema, table.Name),
		strings.Join(cols, ",\n"))
}

func (d *cassandraDDLDialect) AddColumn(tableName string, col sdata.DBColumn) string {
	tableRef := cassandraTableRef(col.Schema, tableName)
	return fmt.Sprintf("ALTER TABLE %s ADD %s %s;",
		tableRef,
		d.QuoteIdentifier(col.Name),
		d.mapColumnType(col))
}

func (d *cassandraDDLDialect) DropColumn(tableName, colName string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP %s;",
		cassandraTableRef("", tableName),
		d.QuoteIdentifier(colName))
}

func (d *cassandraDDLDialect) DropTable(tableName string) string {
	return fmt.Sprintf("DROP TABLE %s;", cassandraTableRef("", tableName))
}

func (d *cassandraDDLDialect) AddForeignKey(_ string, _ sdata.DBColumn) string {
	return ""
}

func (d *cassandraDDLDialect) CreateSearchIndex(_ string, _ sdata.DBColumn) string {
	return ""
}

func (d *cassandraDDLDialect) CreateUniqueIndex(_ string, _ sdata.DBColumn) string {
	return ""
}

func (d *cassandraDDLDialect) CreateIndex(_ string, _ sdata.DBColumn) string {
	return ""
}

func (d *cassandraDDLDialect) AlterClusteringKey(_ string, _ []string) string {
	return ""
}

func cassandraQualifiedTableName(schema, table string) string {
	d := &cassandraDDLDialect{}
	if strings.TrimSpace(schema) == "" {
		return d.QuoteIdentifier(table)
	}
	return d.QuoteIdentifier(schema) + "." + d.QuoteIdentifier(table)
}

func cassandraTableRef(schema, table string) string {
	schema = cassandraTrimIdent(schema)
	table = strings.TrimSpace(table)
	if schema != "" {
		return cassandraQualifiedTableName(schema, cassandraTrimIdent(table))
	}
	parts := strings.Split(table, ".")
	if len(parts) == 2 {
		return cassandraQualifiedTableName(cassandraTrimIdent(parts[0]), cassandraTrimIdent(parts[1]))
	}
	return cassandraQualifiedTableName("", cassandraTrimIdent(table))
}

func cassandraTrimIdent(s string) string {
	return strings.Trim(strings.TrimSpace(s), "`\"")
}

// SQLite dialect
type sqliteDialect struct{}

func (d *sqliteDialect) Name() string { return "sqlite" }

func (d *sqliteDialect) QuoteIdentifier(s string) string {
	return `"` + s + `"`
}

func (d *sqliteDialect) MapType(graphqlType string, notNull bool, primaryKey bool) string {
	t := strings.ToLower(graphqlType)

	if primaryKey {
		switch t {
		case "int", "integer", "bigint", "big int", "smallint", "small int":
			return "INTEGER PRIMARY KEY AUTOINCREMENT"
		default:
			return d.mapBaseType(t) + " PRIMARY KEY"
		}
	}

	baseType := d.mapBaseType(t)
	if notNull {
		return baseType + " NOT NULL"
	}
	return baseType
}

func (d *sqliteDialect) mapBaseType(t string) string {
	// Handle type aliases with embedded sizes (SQLite doesn't use sizes but we parse anyway)
	if baseType, _ := parseTypeWithSize(t); baseType != "" {
		switch baseType {
		case "varchar", "char", "decimal", "numeric":
			return "TEXT"
		}
	}

	switch t {
	case "int", "integer", "bigint", "big int", "smallint", "small int":
		return "INTEGER"
	case "float", "real", "double", "double precision", "decimal", "numeric":
		return "REAL"
	case "boolean", "bool":
		return "INTEGER"
	case "text", "string", "varchar", "character varying", "char", "character":
		return "TEXT"
	case "timestamp", "timestamp with time zone", "timestamptz", "timestamp without time zone":
		return "TEXT"
	case "date", "time", "time with time zone", "timetz", "time without time zone":
		return "TEXT"
	case "interval":
		return "TEXT"
	case "json", "jsonb":
		return "TEXT"
	case "uuid":
		return "TEXT"
	case "bytea", "bytes":
		return "BLOB"
	case "money":
		return "REAL"
	case "xml":
		return "TEXT"
	case "serial", "bigserial", "big serial":
		return "INTEGER"
	default:
		return "TEXT"
	}
}

func (d *sqliteDialect) MapDefault(defaultVal string) string {
	return defaultVal
}

func (d *sqliteDialect) CreateTable(table sdata.DBTable) string {
	var cols []string
	var constraints []string

	for _, col := range table.Columns {
		colDef := fmt.Sprintf("  %s %s",
			d.QuoteIdentifier(col.Name),
			d.MapType(col.Type, col.NotNull, col.PrimaryKey))
		if col.Default != "" {
			colDef += fmt.Sprintf(" DEFAULT %s", d.MapDefault(col.Default))
		}
		cols = append(cols, colDef)

		if col.FKeyTable != "" && col.FKeyCol != "" {
			fkDef := fmt.Sprintf("  FOREIGN KEY (%s) REFERENCES %s(%s)",
				d.QuoteIdentifier(col.Name),
				d.QuoteIdentifier(col.FKeyTable),
				d.QuoteIdentifier(col.FKeyCol))
			if col.FKOnDelete != "" {
				fkDef += fmt.Sprintf(" ON DELETE %s", col.FKOnDelete)
			}
			if col.FKOnUpdate != "" {
				fkDef += fmt.Sprintf(" ON UPDATE %s", col.FKOnUpdate)
			}
			constraints = append(constraints, fkDef)
		}
	}

	tableParts := append(cols, constraints...)
	return fmt.Sprintf("CREATE TABLE %s (\n%s\n);",
		d.QuoteIdentifier(table.Name),
		strings.Join(tableParts, ",\n"))
}

func (d *sqliteDialect) AddColumn(tableName string, col sdata.DBColumn) string {
	colDef := d.MapType(col.Type, col.NotNull, false)
	if col.Default != "" {
		colDef += fmt.Sprintf(" DEFAULT %s", d.MapDefault(col.Default))
	}
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(col.Name),
		colDef)
}

func (d *sqliteDialect) DropColumn(tableName, colName string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(colName))
}

func (d *sqliteDialect) DropTable(tableName string) string {
	return fmt.Sprintf("DROP TABLE %s;", d.QuoteIdentifier(tableName))
}

func (d *sqliteDialect) AddForeignKey(tableName string, col sdata.DBColumn) string {
	return "" // SQLite doesn't support adding FK constraints after table creation
}

func (d *sqliteDialect) CreateSearchIndex(tableName string, col sdata.DBColumn) string {
	return "" // SQLite FTS5 requires virtual table setup
}

func (d *sqliteDialect) CreateUniqueIndex(tableName string, col sdata.DBColumn) string {
	idxName := fmt.Sprintf("idx_%s_%s_unique", tableName, col.Name)
	return fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s);",
		d.QuoteIdentifier(idxName),
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(col.Name))
}

func (d *sqliteDialect) CreateIndex(tableName string, col sdata.DBColumn) string {
	idxName := col.IndexName
	if idxName == "" {
		idxName = fmt.Sprintf("idx_%s_%s", tableName, col.Name)
	}
	return fmt.Sprintf("CREATE INDEX %s ON %s (%s);",
		d.QuoteIdentifier(idxName),
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(col.Name))
}

func (d *sqliteDialect) AlterClusteringKey(_ string, _ []string) string {
	return ""
}

// MSSQL dialect
type mssqlDialect struct{}

func (d *mssqlDialect) Name() string { return "mssql" }

func (d *mssqlDialect) QuoteIdentifier(s string) string {
	return "[" + s + "]"
}

func (d *mssqlDialect) MapType(graphqlType string, notNull bool, primaryKey bool) string {
	t := strings.ToLower(graphqlType)

	if primaryKey {
		switch t {
		case "int", "integer", "bigint", "big int":
			return "BIGINT IDENTITY(1,1) PRIMARY KEY"
		case "smallint", "small int":
			return "INT IDENTITY(1,1) PRIMARY KEY"
		default:
			return d.mapBaseType(t) + " PRIMARY KEY"
		}
	}

	baseType := d.mapBaseType(t)
	if notNull {
		return baseType + " NOT NULL"
	}
	return baseType
}

func (d *mssqlDialect) mapBaseType(t string) string {
	// Handle type aliases with embedded sizes
	if baseType, size := parseTypeWithSize(t); size != "" {
		switch baseType {
		case "varchar":
			return fmt.Sprintf("NVARCHAR(%s)", size)
		case "char":
			return fmt.Sprintf("NCHAR(%s)", size)
		case "decimal", "numeric":
			return fmt.Sprintf("DECIMAL(%s)", size)
		}
	}

	switch t {
	case "int", "integer":
		return "INT"
	case "bigint", "big int":
		return "BIGINT"
	case "smallint", "small int":
		return "SMALLINT"
	case "float", "real":
		return "REAL"
	case "double", "double precision":
		return "FLOAT"
	case "decimal", "numeric":
		return "DECIMAL(10,2)"
	case "boolean", "bool":
		return "BIT"
	case "text", "string":
		return "NVARCHAR(MAX)"
	case "varchar", "character varying":
		return "NVARCHAR(255)"
	case "char", "character":
		return "NCHAR(1)"
	case "timestamp", "timestamp with time zone", "timestamptz":
		return "DATETIMEOFFSET"
	case "timestamp without time zone":
		return "DATETIME2"
	case "date":
		return "DATE"
	case "time", "time with time zone", "timetz":
		return "TIME"
	case "time without time zone":
		return "TIME"
	case "interval":
		return "VARCHAR(255)"
	case "json", "jsonb":
		return "NVARCHAR(MAX)"
	case "uuid":
		return "UNIQUEIDENTIFIER"
	case "bytea", "bytes":
		return "VARBINARY(MAX)"
	case "geometry":
		return "GEOMETRY"
	case "geography":
		return "GEOGRAPHY"
	case "money":
		return "MONEY"
	case "xml":
		return "XML"
	case "serial":
		return "INT IDENTITY(1,1)"
	case "bigserial", "big serial":
		return "BIGINT IDENTITY(1,1)"
	default:
		return "NVARCHAR(MAX)"
	}
}

func (d *mssqlDialect) MapDefault(defaultVal string) string {
	return defaultVal
}

func (d *mssqlDialect) CreateTable(table sdata.DBTable) string {
	var cols []string
	var constraints []string

	for _, col := range table.Columns {
		colDef := fmt.Sprintf("  %s %s",
			d.QuoteIdentifier(col.Name),
			d.MapType(col.Type, col.NotNull, col.PrimaryKey))
		if col.Default != "" {
			colDef += fmt.Sprintf(" DEFAULT %s", d.MapDefault(col.Default))
		}
		cols = append(cols, colDef)

		if col.FKeyTable != "" && col.FKeyCol != "" {
			fkName := fmt.Sprintf("FK_%s_%s", table.Name, col.Name)
			fkDef := fmt.Sprintf("  CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s)",
				d.QuoteIdentifier(fkName),
				d.QuoteIdentifier(col.Name),
				d.QuoteIdentifier(col.FKeyTable),
				d.QuoteIdentifier(col.FKeyCol))
			if col.FKOnDelete != "" {
				fkDef += fmt.Sprintf(" ON DELETE %s", col.FKOnDelete)
			}
			if col.FKOnUpdate != "" {
				fkDef += fmt.Sprintf(" ON UPDATE %s", col.FKOnUpdate)
			}
			constraints = append(constraints, fkDef)
		}
	}

	tableParts := append(cols, constraints...)
	return fmt.Sprintf("CREATE TABLE %s (\n%s\n);",
		d.QuoteIdentifier(table.Name),
		strings.Join(tableParts, ",\n"))
}

func (d *mssqlDialect) AddColumn(tableName string, col sdata.DBColumn) string {
	colDef := d.MapType(col.Type, col.NotNull, false)
	if col.Default != "" {
		colDef += fmt.Sprintf(" DEFAULT %s", d.MapDefault(col.Default))
	}
	return fmt.Sprintf("ALTER TABLE %s ADD %s %s;",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(col.Name),
		colDef)
}

func (d *mssqlDialect) DropColumn(tableName, colName string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(colName))
}

func (d *mssqlDialect) DropTable(tableName string) string {
	return fmt.Sprintf("DROP TABLE %s;", d.QuoteIdentifier(tableName))
}

func (d *mssqlDialect) AddForeignKey(tableName string, col sdata.DBColumn) string {
	if col.FKeyTable == "" || col.FKeyCol == "" {
		return ""
	}
	fkName := fmt.Sprintf("FK_%s_%s", tableName, col.Name)
	sql := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s)",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(fkName),
		d.QuoteIdentifier(col.Name),
		d.QuoteIdentifier(col.FKeyTable),
		d.QuoteIdentifier(col.FKeyCol))
	if col.FKOnDelete != "" {
		sql += fmt.Sprintf(" ON DELETE %s", col.FKOnDelete)
	}
	if col.FKOnUpdate != "" {
		sql += fmt.Sprintf(" ON UPDATE %s", col.FKOnUpdate)
	}
	return sql + ";"
}

func (d *mssqlDialect) CreateSearchIndex(tableName string, col sdata.DBColumn) string {
	return "" // MSSQL full-text requires catalog setup
}

func (d *mssqlDialect) CreateUniqueIndex(tableName string, col sdata.DBColumn) string {
	idxName := fmt.Sprintf("IX_%s_%s_unique", tableName, col.Name)
	return fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s);",
		d.QuoteIdentifier(idxName),
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(col.Name))
}

func (d *mssqlDialect) CreateIndex(tableName string, col sdata.DBColumn) string {
	idxName := col.IndexName
	if idxName == "" {
		idxName = fmt.Sprintf("IX_%s_%s", tableName, col.Name)
	}
	return fmt.Sprintf("CREATE INDEX %s ON %s (%s);",
		d.QuoteIdentifier(idxName),
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(col.Name))
}

func (d *mssqlDialect) AlterClusteringKey(_ string, _ []string) string {
	return ""
}

// Oracle dialect
type oracleDialect struct{}

func (d *oracleDialect) Name() string { return "oracle" }

func (d *oracleDialect) QuoteIdentifier(s string) string {
	return `"` + strings.ToUpper(s) + `"`
}

func (d *oracleDialect) MapType(graphqlType string, notNull bool, primaryKey bool) string {
	t := strings.ToLower(graphqlType)

	if primaryKey {
		switch t {
		case "int", "integer", "bigint", "big int":
			return "NUMBER(19) GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY"
		case "smallint", "small int":
			return "NUMBER(10) GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY"
		default:
			return d.mapBaseType(t) + " PRIMARY KEY"
		}
	}

	baseType := d.mapBaseType(t)
	if notNull {
		return baseType + " NOT NULL"
	}
	return baseType
}

func (d *oracleDialect) mapBaseType(t string) string {
	// Handle type aliases with embedded sizes
	if baseType, size := parseTypeWithSize(t); size != "" {
		switch baseType {
		case "varchar":
			return fmt.Sprintf("VARCHAR2(%s)", size)
		case "char":
			return fmt.Sprintf("CHAR(%s)", size)
		case "decimal", "numeric":
			return fmt.Sprintf("NUMBER(%s)", size)
		}
	}

	switch t {
	case "int", "integer":
		return "NUMBER(10)"
	case "bigint", "big int":
		return "NUMBER(19)"
	case "smallint", "small int":
		return "NUMBER(5)"
	case "float", "real":
		return "BINARY_FLOAT"
	case "double", "double precision":
		return "BINARY_DOUBLE"
	case "decimal", "numeric":
		return "NUMBER(10,2)"
	case "boolean", "bool":
		return "NUMBER(1)"
	case "text", "string":
		return "CLOB"
	case "varchar", "character varying":
		return "VARCHAR2(255)"
	case "char", "character":
		return "CHAR(1)"
	case "timestamp", "timestamp with time zone", "timestamptz":
		return "TIMESTAMP WITH TIME ZONE"
	case "timestamp without time zone":
		return "TIMESTAMP"
	case "date":
		return "DATE"
	case "time", "time with time zone", "timetz":
		return "TIMESTAMP WITH TIME ZONE"
	case "time without time zone":
		return "TIMESTAMP"
	case "interval":
		return "INTERVAL DAY TO SECOND"
	case "json", "jsonb":
		return "CLOB"
	case "uuid":
		return "RAW(16)"
	case "bytea", "bytes":
		return "BLOB"
	case "money":
		return "NUMBER(19,4)"
	case "xml":
		return "XMLTYPE"
	case "serial":
		return "NUMBER(10) GENERATED BY DEFAULT AS IDENTITY"
	case "bigserial", "big serial":
		return "NUMBER(19) GENERATED BY DEFAULT AS IDENTITY"
	default:
		return "CLOB"
	}
}

func (d *oracleDialect) MapDefault(defaultVal string) string {
	return defaultVal
}

func (d *oracleDialect) CreateTable(table sdata.DBTable) string {
	var cols []string
	var constraints []string

	for _, col := range table.Columns {
		colDef := fmt.Sprintf("  %s %s",
			d.QuoteIdentifier(col.Name),
			d.MapType(col.Type, col.NotNull, col.PrimaryKey))
		if col.Default != "" {
			colDef += fmt.Sprintf(" DEFAULT %s", d.MapDefault(col.Default))
		}
		cols = append(cols, colDef)

		if col.FKeyTable != "" && col.FKeyCol != "" {
			fkName := fmt.Sprintf("FK_%s_%s", strings.ToUpper(table.Name), strings.ToUpper(col.Name))
			fkDef := fmt.Sprintf("  CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s)",
				d.QuoteIdentifier(fkName),
				d.QuoteIdentifier(col.Name),
				d.QuoteIdentifier(col.FKeyTable),
				d.QuoteIdentifier(col.FKeyCol))
			if col.FKOnDelete != "" {
				fkDef += fmt.Sprintf(" ON DELETE %s", col.FKOnDelete)
			}
			if col.FKOnUpdate != "" {
				fkDef += fmt.Sprintf(" ON UPDATE %s", col.FKOnUpdate)
			}
			constraints = append(constraints, fkDef)
		}
	}

	tableParts := append(cols, constraints...)
	return fmt.Sprintf("CREATE TABLE %s (\n%s\n)",
		d.QuoteIdentifier(table.Name),
		strings.Join(tableParts, ",\n"))
}

func (d *oracleDialect) AddColumn(tableName string, col sdata.DBColumn) string {
	colDef := d.MapType(col.Type, col.NotNull, false)
	if col.Default != "" {
		colDef += fmt.Sprintf(" DEFAULT %s", d.MapDefault(col.Default))
	}
	return fmt.Sprintf("ALTER TABLE %s ADD %s %s",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(col.Name),
		colDef)
}

func (d *oracleDialect) DropColumn(tableName, colName string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(colName))
}

func (d *oracleDialect) DropTable(tableName string) string {
	return fmt.Sprintf("DROP TABLE %s", d.QuoteIdentifier(tableName))
}

func (d *oracleDialect) AddForeignKey(tableName string, col sdata.DBColumn) string {
	if col.FKeyTable == "" || col.FKeyCol == "" {
		return ""
	}
	fkName := fmt.Sprintf("FK_%s_%s", strings.ToUpper(tableName), strings.ToUpper(col.Name))
	sql := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s)",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(fkName),
		d.QuoteIdentifier(col.Name),
		d.QuoteIdentifier(col.FKeyTable),
		d.QuoteIdentifier(col.FKeyCol))
	if col.FKOnDelete != "" {
		sql += fmt.Sprintf(" ON DELETE %s", col.FKOnDelete)
	}
	// Oracle doesn't support ON UPDATE in foreign keys directly
	return sql
}

func (d *oracleDialect) CreateSearchIndex(tableName string, col sdata.DBColumn) string {
	return "" // Oracle Text requires context index setup
}

func (d *oracleDialect) CreateUniqueIndex(tableName string, col sdata.DBColumn) string {
	idxName := fmt.Sprintf("IX_%s_%s_UQ", strings.ToUpper(tableName), strings.ToUpper(col.Name))
	return fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)",
		d.QuoteIdentifier(idxName),
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(col.Name))
}

func (d *oracleDialect) CreateIndex(tableName string, col sdata.DBColumn) string {
	idxName := col.IndexName
	if idxName == "" {
		idxName = fmt.Sprintf("IX_%s_%s", strings.ToUpper(tableName), strings.ToUpper(col.Name))
	}
	return fmt.Sprintf("CREATE INDEX %s ON %s (%s)",
		d.QuoteIdentifier(idxName),
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(col.Name))
}

func (d *oracleDialect) AlterClusteringKey(_ string, _ []string) string {
	return ""
}

// parseTypeWithSize extracts base type and size from type aliases like "Varchar255" or "Decimal10_2"
// Also handles types with parentheses like "numeric(7,2)" from @type(args: "7,2") directive
func parseTypeWithSize(typeName string) (baseType string, size string) {
	typeName = strings.ToLower(typeName)

	// Check for common patterns
	patterns := []struct {
		prefix string
		base   string
	}{
		{"varchar", "varchar"},
		{"char", "char"},
		{"decimal", "decimal"},
		{"numeric", "numeric"},
	}

	for _, p := range patterns {
		if strings.HasPrefix(typeName, p.prefix) {
			suffix := typeName[len(p.prefix):]
			if suffix == "" {
				return "", ""
			}
			// Strip parentheses if present (from @type(args: "7,2") -> "numeric(7,2)")
			suffix = strings.TrimPrefix(suffix, "(")
			suffix = strings.TrimSuffix(suffix, ")")
			// Convert underscore to comma for decimal types (e.g., "10_2" -> "10,2")
			size = strings.ReplaceAll(suffix, "_", ",")
			return p.base, size
		}
	}

	return "", ""
}
