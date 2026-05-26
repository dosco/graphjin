package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/jsn"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

const (
	CacheSourceDB      = "db"
	CacheSourceCodeSQL = "codesql"
	CacheSourceFS      = "fs"
	CacheSourceRemote  = "remote"

	CacheKindRow      = "row"
	CacheKindTable    = "table"
	CacheKindKey      = "key"
	CacheKindPrefix   = "prefix"
	CacheKindResolver = "resolver"
)

// RowRef represents a source-owned cache dependency. The legacy DB shape
// ({Table, ID}) is still valid and normalizes to db/row or db/table refs.
type RowRef struct {
	Source string
	Scope  string
	Kind   string
	Table  string
	ID     string
}

// ResponseProcessor handles extraction and stripping of __gj_id fields for caching
type ResponseProcessor struct {
	qc *qcode.QCode
}

// NewResponseProcessor creates a new response processor
func NewResponseProcessor(qc *qcode.QCode) *ResponseProcessor {
	return &ResponseProcessor{qc: qc}
}

// DBRowRef builds a row-level database cache ref.
func DBRowRef(database, table, id string) RowRef {
	return RowRef{Source: CacheSourceDB, Scope: database, Kind: CacheKindRow, Table: table, ID: id}
}

// DBTableRef builds a table-level database cache ref.
func DBTableRef(database, table string) RowRef {
	return RowRef{Source: CacheSourceDB, Scope: database, Kind: CacheKindTable, Table: table}
}

// CodeSQLTableRefs builds table refs for CodeSQL-managed source changes.
func CodeSQLTableRefs(database string, tables []string) []RowRef {
	refs := make([]RowRef, 0, len(tables))
	for _, table := range tables {
		table = strings.TrimSpace(table)
		if table == "" {
			continue
		}
		refs = append(refs, RowRef{
			Source: CacheSourceCodeSQL,
			Scope:  database,
			Kind:   CacheKindTable,
			Table:  table,
		})
	}
	return refs
}

// RemoteResolverRef builds a cache ref for an external resolver/API result.
func RemoteResolverRef(scope, id string) RowRef {
	return RowRef{Source: CacheSourceRemote, Scope: scope, Kind: CacheKindResolver, ID: id}
}

// RemoteResolverRefs builds resolver refs for one or more external IDs.
func RemoteResolverRefs(scope string, ids ...string) []RowRef {
	refs := make([]RowRef, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, RemoteResolverRef(scope, id))
	}
	return refs
}

// FilesystemKeyRefs builds refs affected by a write/delete to key. Directory
// prefixes are included so list fragments for common prefix queries are evicted.
func FilesystemKeyRefs(name, key string) []RowRef {
	key = normalizeFilesystemCacheID(key)
	refs := []RowRef{{
		Source: CacheSourceFS,
		Scope:  name,
		Kind:   CacheKindKey,
		ID:     key,
	}}

	seen := map[string]struct{}{}
	addPrefix := func(prefix string) {
		prefix = normalizeFilesystemCacheID(prefix)
		if _, ok := seen[prefix]; ok {
			return
		}
		seen[prefix] = struct{}{}
		refs = append(refs, RowRef{
			Source: CacheSourceFS,
			Scope:  name,
			Kind:   CacheKindPrefix,
			ID:     prefix,
		})
	}
	addPrefixVariants := func(prefix string) {
		addPrefix(prefix)
		if strings.HasSuffix(prefix, "/") {
			addPrefix(strings.TrimSuffix(prefix, "/"))
		}
	}

	addPrefix("")
	parts := strings.Split(key, "/")
	if len(parts) > 1 {
		var b strings.Builder
		for i := 0; i < len(parts)-1; i++ {
			if parts[i] == "" {
				continue
			}
			b.WriteString(parts[i])
			b.WriteByte('/')
			addPrefixVariants(b.String())
		}
	}
	return refs
}

// FilesystemPrefixRefs builds refs for a filesystem list prefix. The root
// prefix is included so broad filesystem invalidations can evict all entries.
func FilesystemPrefixRefs(name, prefix string) []RowRef {
	prefix = normalizeFilesystemCacheID(prefix)
	refs := []RowRef{filesystemPrefixRef(name, prefix)}
	if strings.HasSuffix(prefix, "/") {
		refs = append(refs, filesystemPrefixRef(name, strings.TrimSuffix(prefix, "/")))
	} else if prefix != "" {
		refs = append(refs, filesystemPrefixRef(name, prefix+"/"))
	}
	if prefix != "" {
		refs = append(refs, filesystemPrefixRef(name, ""))
	}
	return refs
}

func filesystemKeyRef(name, key string) RowRef {
	return RowRef{
		Source: CacheSourceFS,
		Scope:  name,
		Kind:   CacheKindKey,
		ID:     normalizeFilesystemCacheID(key),
	}
}

func filesystemPrefixRef(name, prefix string) RowRef {
	return RowRef{
		Source: CacheSourceFS,
		Scope:  name,
		Kind:   CacheKindPrefix,
		ID:     normalizeFilesystemCacheID(prefix),
	}
}

// FilesystemPrefixRef builds a cache ref for a filesystem list prefix.
func FilesystemPrefixRef(name, prefix string) RowRef {
	return filesystemPrefixRef(name, prefix)
}

func normalizeFilesystemCacheID(id string) string {
	id = strings.ReplaceAll(id, "\\", "/")
	return strings.TrimLeft(id, "/")
}

// Normalize returns the canonical source/kind form for this ref.
func (r RowRef) Normalize() RowRef {
	if r.Source == "" {
		r.Source = CacheSourceDB
	}
	if r.Kind == "" {
		if r.ID == "" {
			r.Kind = CacheKindTable
		} else {
			r.Kind = CacheKindRow
		}
	}
	return r
}

// DependencyKey returns the exact index key for this dependency ref.
func (r RowRef) DependencyKey() string {
	r = r.Normalize()
	return strings.Join([]string{
		escapeCachePart(r.Source),
		escapeCachePart(r.Scope),
		escapeCachePart(r.Kind),
		escapeCachePart(r.Table),
		escapeCachePart(r.ID),
	}, ":")
}

// TableDependency returns a table-level dependency matching this ref's source.
func (r RowRef) TableDependency() (RowRef, bool) {
	r = r.Normalize()
	if r.Table == "" {
		return RowRef{}, false
	}
	if r.Kind == CacheKindTable {
		return r, true
	}
	switch r.Source {
	case CacheSourceDB, CacheSourceCodeSQL:
		return RowRef{Source: r.Source, Scope: r.Scope, Kind: CacheKindTable, Table: r.Table}, true
	default:
		return RowRef{}, false
	}
}

func escapeCachePart(s string) string {
	if s == "" {
		return "-"
	}
	return url.QueryEscape(s)
}

// ProcessForCache extracts row references and strips __gj_id from response.
// Returns the cleaned response and list of (table, row_id) pairs.
func (rp *ResponseProcessor) ProcessForCache(data []byte) (cleaned []byte, refs []RowRef, err error) {
	if len(data) == 0 {
		return data, nil, nil
	}
	if rp.qc == nil {
		return data, nil, nil
	}

	// Parse JSON response
	var result map[string]interface{}
	if err = json.Unmarshal(data, &result); err != nil {
		return data, nil, err
	}

	dataMap, ok := responseDataMap(result)
	if !ok {
		return data, nil, nil
	}

	refs = make([]RowRef, 0, 100)

	// Process each root selection
	for i := range rp.qc.Selects {
		sel := &rp.qc.Selects[i]
		if sel.ParentID != -1 {
			continue // Skip non-root selections
		}

		fieldName := sel.FieldName
		if fieldName == "" {
			fieldName = sel.Table
		}

		if fieldData, ok := dataMap[fieldName]; ok {
			rp.processNode(sel.Table, fieldData, &refs, sel)
		}
	}

	cleaned, err = stripCacheTrackingFields(data)
	return
}

func stripCacheTrackingFields(data []byte) ([]byte, error) {
	fields := jsn.Get(data, [][]byte{[]byte("__gj_id")})
	if len(fields) == 0 {
		return data, nil
	}

	to := make([]jsn.Field, len(fields))
	var buf bytes.Buffer
	buf.Grow(len(data))
	if err := jsn.Replace(&buf, data, fields, to); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (rp *ResponseProcessor) processNode(
	tableName string,
	data interface{},
	refs *[]RowRef,
	sel *qcode.Select,
) {
	switch v := data.(type) {
	case map[string]interface{}:
		rp.processObject(tableName, v, refs, sel)
	case []interface{}:
		for _, item := range v {
			if obj, ok := item.(map[string]interface{}); ok {
				rp.processObject(tableName, obj, refs, sel)
			}
		}
	}
}

func (rp *ResponseProcessor) processObject(
	tableName string,
	obj map[string]interface{},
	refs *[]RowRef,
	sel *qcode.Select,
) {
	// Extract and remove __gj_id
	if id, ok := obj["__gj_id"]; ok {
		*refs = append(*refs, RowRef{
			Table: tableName,
			ID:    stringifyID(id),
		})
		delete(obj, "__gj_id")
	} else if id, ok := primaryKeyValueFromObject(obj, sel); ok {
		*refs = append(*refs, RowRef{
			Table: tableName,
			ID:    stringifyID(id),
		})
	}

	// Process child selections
	if sel != nil {
		for _, childID := range sel.Children {
			if childID < 0 || int(childID) >= len(rp.qc.Selects) {
				continue
			}
			childSel := &rp.qc.Selects[childID]

			fieldName := childSel.FieldName
			if fieldName == "" {
				fieldName = childSel.Table
			}

			if childData, ok := obj[fieldName]; ok {
				rp.processNode(childSel.Table, childData, refs, childSel)
			}
		}
	}
}

func responseDataMap(result map[string]interface{}) (map[string]interface{}, bool) {
	if dataField, ok := result["data"]; ok {
		dataMap, ok := dataField.(map[string]interface{})
		return dataMap, ok
	}

	// Fragment caches store GraphJin's inner data object directly
	// (e.g. {"users":[...]}), not the outer {"data": ...} envelope.
	if _, hasErrors := result["errors"]; hasErrors {
		return nil, false
	}
	return result, true
}

func primaryKeyValueFromObject(obj map[string]interface{}, sel *qcode.Select) (interface{}, bool) {
	if sel == nil || sel.Ti.PrimaryCol.Name == "" {
		return nil, false
	}
	pkName := sel.Ti.PrimaryCol.Name
	for _, f := range sel.Fields {
		if f.Type != qcode.FieldTypeCol || !strings.EqualFold(f.Col.Name, pkName) {
			continue
		}
		fieldName := f.FieldName
		if fieldName == "" {
			fieldName = f.Col.Name
		}
		id, ok := obj[fieldName]
		return id, ok
	}
	return nil, false
}

// stringifyID converts various ID types to string
func stringifyID(id interface{}) string {
	switch v := id.(type) {
	case string:
		return v
	case float64:
		// Check if it's a whole number
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case json.Number:
		return v.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ExtractMutationRefs extracts affected row IDs from a mutation response.
// Used for cache invalidation after INSERT/UPDATE/DELETE.
func ExtractMutationRefs(qc *qcode.QCode, data []byte) []RowRef {
	if len(data) == 0 || qc == nil {
		return nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}

	dataField, ok := responseDataMap(result)
	if !ok {
		return nil
	}

	refs := make([]RowRef, 0)

	// Extract IDs from each mutated table using the Mutates list
	for _, m := range qc.Mutates {
		if m.Type == qcode.MTNone {
			continue
		}

		tableName := m.Ti.Name
		pkName := m.Ti.PrimaryCol.Name
		if pkName == "" {
			continue
		}

		// Find the table data in response using the mutation key
		if tableData, ok := dataField[m.Key]; ok {
			refs = append(refs, extractIDsFromData(tableName, pkName, tableData)...)
		}
	}

	return refs
}

func extractIDsFromData(tableName, pkName string, data interface{}) []RowRef {
	refs := make([]RowRef, 0)

	switch v := data.(type) {
	case map[string]interface{}:
		if id, ok := v[pkName]; ok {
			refs = append(refs, RowRef{Table: tableName, ID: stringifyID(id)})
		}
		// Also check for __gj_id in case it was added
		if id, ok := v["__gj_id"]; ok {
			refs = append(refs, RowRef{Table: tableName, ID: stringifyID(id)})
		}
	case []interface{}:
		for _, item := range v {
			if obj, ok := item.(map[string]interface{}); ok {
				if id, ok := obj[pkName]; ok {
					refs = append(refs, RowRef{Table: tableName, ID: stringifyID(id)})
				}
				if id, ok := obj["__gj_id"]; ok {
					refs = append(refs, RowRef{Table: tableName, ID: stringifyID(id)})
				}
			}
		}
	}

	return refs
}
