package redshift

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"sync"

	"github.com/dosco/graphjin/tests/v3/hostedemu"
)

type Adapter struct{}

func NewAdapter() Adapter {
	return Adapter{}
}

func (Adapter) Name() string {
	return "redshift"
}

func (Adapter) DefaultSeedPath() string {
	return "redshift.sql"
}

func (Adapter) ParseSeed(seedSQL string) (any, error) {
	return ParseSeedBytes([]byte(seedSQL))
}

func (Adapter) NewSession(c any) hostedemu.Session {
	schema, _ := c.(*Schema)
	return &Session{schema: schema}
}

func (Adapter) TranslateSetup(seedSQL string, c any) ([]string, error) {
	schema, ok := c.(*Schema)
	if !ok || schema == nil {
		return nil, fmt.Errorf("redshift emulator: invalid catalog %T", c)
	}
	return TranslateSetup(seedSQL, schema)
}

func (Adapter) TranslateDiscoveryQuery(sql string, args []driver.NamedValue, c any) (string, []driver.NamedValue, error) {
	schema, _ := c.(*Schema)
	return TranslateDiscoveryQuery(sql, args, schema)
}

func (Adapter) TranslateDiscoveryExec(sql string, args []driver.NamedValue, c any) ([]string, []driver.NamedValue, error) {
	translated, translatedArgs, err := Adapter{}.TranslateDiscoveryQuery(sql, args, c)
	return []string{translated}, translatedArgs, err
}

func (Adapter) TranslateMetadataRefresh(c any) ([]string, error) {
	schema, ok := c.(*Schema)
	if !ok || schema == nil {
		return nil, fmt.Errorf("redshift emulator: invalid catalog %T", c)
	}
	return TranslateMetadataRefresh(schema), nil
}

func (Adapter) TranslateRuntime(sql string, args []driver.NamedValue, _ any) (string, []driver.NamedValue, error) {
	return lowerRedshift(sql), args, nil
}

func (Adapter) TranslateDirect(sql string, args []driver.NamedValue, _ any) (string, []driver.NamedValue, error) {
	return lowerRedshiftDirect(sql), args, nil
}

func (Adapter) NormalizeIdentifier(identifier string) string {
	return NormalizeIdentifier(identifier)
}

func (Adapter) MapType(sourceType string) string {
	return MapType(sourceType)
}

func (Adapter) ClassifyPhase(sql string) string {
	upper := strings.ToUpper(hostedemu.NormalizeSQL(sql))
	switch {
	case strings.HasPrefix(upper, "SHOW ") ||
		strings.Contains(upper, "SVV_") ||
		strings.Contains(upper, "PG_TABLE_DEF") ||
		strings.Contains(upper, "CURRENT_SCHEMA") ||
		strings.Contains(upper, "CURRENT_DATABASE"):
		return "discovery"
	case isRuntimeSQL(upper):
		return "runtime"
	case strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH") ||
		strings.HasPrefix(upper, "INSERT") || strings.HasPrefix(upper, "UPDATE") ||
		strings.HasPrefix(upper, "DELETE") || strings.HasPrefix(upper, "CREATE") ||
		strings.HasPrefix(upper, "DROP") || strings.HasPrefix(upper, "ALTER"):
		return "direct"
	default:
		return "unknown"
	}
}

type Session struct {
	schema *Schema
	mu     sync.Mutex
}

func (s *Session) ApplyDDL(sql string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.schema != nil {
		s.schema.ApplyDDL(sql)
	}
}

func (s *Session) PlaceholderQuery(sql string) (*hostedemu.Rows, string, error) {
	norm := hostedemu.NormalizeSQL(sql)
	upper := strings.ToUpper(norm)
	switch {
	case isCountQuery(upper):
		return hostedemu.NewRows([]string{"COUNT"}, []driver.Value{int64(1)}), "rows:1", nil
	case isRuntimeSQL(upper):
		return hostedemu.NewRows([]string{"JSON"}, []driver.Value{[]byte(`{"id":1}`)}), "rows:1", nil
	case strings.HasPrefix(upper, "SHOW DATABASES"):
		return s.placeholderDatabases()
	case strings.HasPrefix(upper, "SHOW SCHEMAS"):
		return s.placeholderSchemas()
	case strings.HasPrefix(upper, "SHOW TABLES"):
		return s.placeholderTables()
	case strings.HasPrefix(upper, "SHOW COLUMNS"):
		return s.placeholderColumns()
	case strings.HasPrefix(upper, "SELECT"):
		cols := selectColumns(norm)
		vals := make([]driver.Value, len(cols))
		for i, col := range cols {
			vals[i] = valueForColumn(col)
		}
		return hostedemu.NewRows(cols, vals), "rows:1", nil
	default:
		return hostedemu.NewRows([]string{"JSON"}, []driver.Value{[]byte(`{"id":1}`)}), "rows:1", nil
	}
}

func (s *Session) placeholderDatabases() (*hostedemu.Rows, string, error) {
	dbName := "dev"
	if s.schema != nil && s.schema.DBName != "" {
		dbName = s.schema.DBName
	}
	return hostedemu.NewRows([]string{"database_name"}, []driver.Value{dbName}), "rows:1", nil
}

func (s *Session) placeholderSchemas() (*hostedemu.Rows, string, error) {
	schemaName := "public"
	dbName := "dev"
	if s.schema != nil {
		dbName = s.schema.DBName
		schemaName = s.schema.Schema
	}
	return hostedemu.NewRows(
		[]string{"database_name", "schema_name", "schema_owner", "schema_type", "schema_acl", "source_database", "schema_option"},
		[]driver.Value{dbName, schemaName, "owner", "local", "", "", ""},
	), "rows:1", nil
}

func (s *Session) placeholderTables() (*hostedemu.Rows, string, error) {
	cols := showTableColumns()
	var vals [][]driver.Value
	if s.schema != nil {
		for _, t := range s.schema.Tables {
			vals = append(vals, tableRowValues(t, cols))
		}
	}
	return hostedemu.NewRows(cols, vals...), fmt.Sprintf("rows:%d", len(vals)), nil
}

func (s *Session) placeholderColumns() (*hostedemu.Rows, string, error) {
	cols := showColumnColumns()
	var vals [][]driver.Value
	if s.schema != nil {
		for _, t := range s.schema.Tables {
			for _, c := range t.Columns {
				vals = append(vals, columnRowValues(t, c, cols))
			}
		}
	}
	return hostedemu.NewRows(cols, vals...), fmt.Sprintf("rows:%d", len(vals)), nil
}

func isCountQuery(upper string) bool {
	return strings.HasPrefix(upper, "SELECT COUNT(") || strings.HasPrefix(upper, "SELECT COUNT(*)")
}

func isRuntimeSQL(upper string) bool {
	return strings.Contains(upper, "JSON_OBJECT") ||
		strings.Contains(upper, "OBJECT_CONSTRUCT") ||
		strings.Contains(upper, "ARRAY_AGG") ||
		strings.Contains(upper, "_GJ_IDS") ||
		strings.Contains(upper, "JSON_PARSE(")
}
