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
}

func getDDLDialect(dbType string) DDLDialect {
	switch dbType {
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
	default:
		return &postgresDialect{}
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

// MariaDB dialect (extends MySQL)
type mariadbDialect struct {
	mysqlDialect
}

func (d *mariadbDialect) Name() string { return "mariadb" }

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
