package snowflake

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"sync"

	"github.com/dosco/graphjin/tests/v3/hostedemu"
	"github.com/dosco/graphjin/tests/v3/hostedemu/snowflake/catalog"
	"github.com/dosco/graphjin/tests/v3/hostedemu/snowflake/translate"
)

type Adapter struct{}

func NewAdapter() Adapter {
	return Adapter{}
}

func (Adapter) Name() string {
	return "snowflake"
}

func (Adapter) DefaultSeedPath() string {
	return "snowflake.sql"
}

func (Adapter) ParseSeed(seedSQL string) (any, error) {
	return catalog.ParseSeedBytes([]byte(seedSQL))
}

func (Adapter) NewSession(c any) hostedemu.Session {
	schema, _ := c.(*catalog.Schema)
	return &Session{schema: schema}
}

func (Adapter) TranslateSetup(seedSQL string, c any) ([]string, error) {
	schema, ok := c.(*catalog.Schema)
	if !ok || schema == nil {
		return nil, fmt.Errorf("snowflake emulator: invalid catalog %T", c)
	}
	return translate.TranslateSetup(seedSQL, schema)
}

func (Adapter) TranslateDiscoveryQuery(sql string, args []driver.NamedValue, c any) (string, []driver.NamedValue, error) {
	schema, _ := c.(*catalog.Schema)
	return translate.TranslateDiscoveryQuery(sql, args, schema)
}

func (Adapter) TranslateDiscoveryExec(sql string, args []driver.NamedValue, c any) ([]string, []driver.NamedValue, error) {
	schema, _ := c.(*catalog.Schema)
	return translate.TranslateDiscoveryExec(sql, args, schema)
}

func (Adapter) TranslateMetadataRefresh(c any) ([]string, error) {
	schema, ok := c.(*catalog.Schema)
	if !ok || schema == nil {
		return nil, fmt.Errorf("snowflake emulator: invalid catalog %T", c)
	}
	return translate.TranslateMetadataRefresh(schema), nil
}

func (Adapter) TranslateRuntime(sql string, args []driver.NamedValue, c any) (string, []driver.NamedValue, error) {
	schema, _ := c.(*catalog.Schema)
	return translate.TranslateRuntime(sql, args, schema)
}

func (Adapter) TranslateDirect(sql string, args []driver.NamedValue, c any) (string, []driver.NamedValue, error) {
	schema, _ := c.(*catalog.Schema)
	return translate.TranslateDirect(sql, args, schema)
}

func (Adapter) NormalizeIdentifier(identifier string) string {
	return translate.NormalizeIdentifier(identifier)
}

func (Adapter) MapType(sourceType string) string {
	return translate.MapType(sourceType)
}

func (Adapter) ClassifyPhase(sql string) string {
	upper := strings.ToUpper(hostedemu.NormalizeSQL(sql))
	switch {
	case strings.Contains(upper, "INFORMATION_SCHEMA") ||
		strings.HasPrefix(upper, "SHOW ") ||
		strings.Contains(upper, "LAST_QUERY_ID") ||
		strings.Contains(upper, "RESULT_SCAN") ||
		strings.Contains(upper, "CURRENT_SCHEMA()") ||
		strings.Contains(upper, "CURRENT_DATABASE()"):
		return "discovery"
	case isRuntimeSQL(upper):
		return "runtime"
	case strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "INSERT") ||
		strings.HasPrefix(upper, "UPDATE") || strings.HasPrefix(upper, "DELETE") ||
		strings.HasPrefix(upper, "CREATE") || strings.HasPrefix(upper, "DROP") ||
		strings.HasPrefix(upper, "ALTER"):
		return "direct"
	default:
		return "unknown"
	}
}

type Session struct {
	schema *catalog.Schema
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

func isCountQuery(upper string) bool {
	return strings.HasPrefix(upper, "SELECT COUNT(") || strings.HasPrefix(upper, "SELECT COUNT(*)")
}

func isRuntimeSQL(upper string) bool {
	return strings.Contains(upper, "OBJECT_CONSTRUCT") ||
		strings.Contains(upper, "ARRAY_AGG") ||
		strings.Contains(upper, "_GJ_IDS") ||
		strings.Contains(upper, "PARSE_JSON(")
}

func selectColumns(sql string) []string {
	if !strings.HasPrefix(strings.ToUpper(sql), "SELECT ") {
		return []string{"JSON"}
	}
	from := indexTopLevelKeyword(sql, "FROM", len("SELECT "))
	if from < 0 {
		return []string{"JSON"}
	}
	items := catalog.SplitTopLevel(sql[len("SELECT "):from], ',')
	cols := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			cols = append(cols, selectColumnName(item))
		}
	}
	if len(cols) == 0 {
		return []string{"JSON"}
	}
	return cols
}

func selectColumnName(expr string) string {
	fields := strings.Fields(expr)
	if len(fields) >= 3 && strings.EqualFold(fields[len(fields)-2], "AS") {
		return catalog.TrimIdent(fields[len(fields)-1])
	}
	if len(fields) != 0 {
		expr = fields[len(fields)-1]
	}
	if dot := strings.LastIndexByte(expr, '.'); dot >= 0 {
		expr = expr[dot+1:]
	}
	return catalog.TrimIdent(expr)
}

func indexTopLevelKeyword(sql, keyword string, start int) int {
	upper := strings.ToUpper(sql)
	needle := strings.ToUpper(keyword)
	depth := 0
	inSingle := false
	inDouble := false
	for i := start; i <= len(sql)-len(needle); i++ {
		ch := sql[i]
		switch ch {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '(':
			if !inSingle && !inDouble {
				depth++
			}
		case ')':
			if !inSingle && !inDouble && depth > 0 {
				depth--
			}
		}
		if inSingle || inDouble || depth != 0 {
			continue
		}
		if strings.HasPrefix(upper[i:], needle) && keywordBoundary(sql, i-1) && keywordBoundary(sql, i+len(needle)) {
			return i
		}
	}
	return -1
}

func keywordBoundary(sql string, idx int) bool {
	if idx < 0 || idx >= len(sql) {
		return true
	}
	ch := sql[idx]
	return !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_')
}

func valueForColumn(col string) driver.Value {
	switch strings.ToLower(catalog.TrimIdent(col)) {
	case "count", "count(*)", "row_count", "product_id":
		return int64(1)
	case "id", "variant_id", "order_id", "customer_id", "owner_id", "subject_id", "quantity":
		return int64(2)
	case "variant_name":
		return "Medium"
	case "sku":
		return "PROD1-M"
	case "price", "amount":
		return float64(24.99)
	case "full_name":
		return "User 1"
	case "email":
		return "user1@test.com"
	case "name":
		return "Product 1"
	case "description":
		return "Description"
	case "country_code", "region":
		return "US"
	case "disabled":
		return false
	case "created_at", "updated_at", "event_time", "returned_at":
		return "2021-01-09 16:37:01"
	case "tags", "category_ids", "metadata", "payload", "category_counts", "validity_period":
		return []byte(`[]`)
	default:
		return "mock"
	}
}
