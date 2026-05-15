package dialect

import (
	"fmt"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

func validateStandardWindowFunction(dialectName string, f qcode.Field, supportsNulls bool) error {
	if f.Window == nil {
		return nil
	}
	if qcode.WindowSpecHasNulls(f.Window) && !supportsNulls {
		return fmt.Errorf("%s does not support this analytics ordering", dialectName)
	}
	return nil
}

func (d *PostgresDialect) ValidateWindowFunction(f qcode.Field) error {
	return validateStandardWindowFunction(d.Name(), f, true)
}

func (d *SnowflakeDialect) ValidateWindowFunction(f qcode.Field) error {
	return validateStandardWindowFunction(d.Name(), f, true)
}

func (d *OracleDialect) ValidateWindowFunction(f qcode.Field) error {
	return validateStandardWindowFunction(d.Name(), f, true)
}

func (d *MySQLDialect) ValidateWindowFunction(f qcode.Field) error {
	if d.DBVersion > 0 && d.DBVersion < 80000 {
		return fmt.Errorf("analytics directive on field %q is not supported by MySQL %d; MySQL 8.0+ is required",
			f.FieldName, d.DBVersion)
	}
	return validateStandardWindowFunction(d.Name(), f, false)
}

func (d *MariaDBDialect) ValidateWindowFunction(f qcode.Field) error {
	if d.DBVersion > 0 && d.DBVersion < 100200 {
		return fmt.Errorf("analytics directive on field %q is not supported by MariaDB %d; MariaDB 10.2+ is required",
			f.FieldName, d.DBVersion)
	}
	return validateStandardWindowFunction(d.Name(), f, false)
}

func (d *SQLiteDialect) ValidateWindowFunction(f qcode.Field) error {
	if sqliteVersionLess(d.DBVersion, 32500, 3025000) {
		return fmt.Errorf("analytics directive on field %q is not supported by SQLite %d; SQLite 3.25+ is required",
			f.FieldName, d.DBVersion)
	}
	if qcode.WindowSpecHasNulls(f.Window) && sqliteVersionLess(d.DBVersion, 33000, 3030000) {
		return fmt.Errorf("SQLite %d does not support this analytics ordering; SQLite 3.30+ is required",
			d.DBVersion)
	}
	return nil
}

func (d *MSSQLDialect) ValidateWindowFunction(f qcode.Field) error {
	if mssqlVersionLessThan2012(d.DBVersion) {
		return fmt.Errorf("analytics directive on field %q is not supported by MSSQL %d; SQL Server 2012+ is required",
			f.FieldName, d.DBVersion)
	}
	return validateStandardWindowFunction(d.Name(), f, false)
}

func (d *MongoDBDialect) ValidateWindowFunction(f qcode.Field) error {
	return fmt.Errorf("analytics directive on field %q is not supported by MongoDB", f.FieldName)
}

func sqliteVersionLess(v, compactMin, libMin int) bool {
	if v == 0 {
		return false
	}
	if v >= 1000000 {
		return v < libMin
	}
	return v < compactMin
}

func mssqlVersionLessThan2012(v int) bool {
	if v == 0 {
		return false
	}
	if v < 100 {
		return v < 11
	}
	if v < 10000 {
		return v < 2012
	}
	return v < 110000
}
