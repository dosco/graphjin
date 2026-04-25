package serv

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"

	core "github.com/dosco/graphjin/core/v3"
	"golang.org/x/sync/singleflight"
)

type DiscoveryManager struct {
	gj *core.GraphJin

	tablesCache    sync.Map
	fullCache      sync.Map
	insightsCache  sync.Map
	profileCache   sync.Map
	namespaceCache sync.Map // database -> []NamespaceRollup
	rowCountCache  sync.Map // database + "\x00" + schema -> map[string]int64

	tablesGroup    singleflight.Group
	fullGroup      singleflight.Group
	insightsGroup  singleflight.Group
	profileGroup   singleflight.Group
	namespaceGroup singleflight.Group
	rowCountGroup  singleflight.Group
}

type dbTablesPayload struct {
	Tables           []TableIndexEntry
	DatabaseOverview DatabaseOverview
}

type dbFullPayload struct {
	Tables           []TableDetailEntry
	DatabaseOverview DatabaseOverview
	Profiles         map[string]*TableProfile
}

func NewDiscoveryManager(gj *core.GraphJin) *DiscoveryManager {
	dm := &DiscoveryManager{gj: gj}
	gj.OnSchemaChange(func(dbName, hash string) {
		dm.Invalidate()
	})
	return dm
}

func getSchemas(gj *core.GraphJin, database string) []*core.TableSchema {
	allTables := gj.GetTablesForDatabase(database)
	sort.Slice(allTables, func(i, j int) bool {
		if allTables[i].Schema != allTables[j].Schema {
			return allTables[i].Schema < allTables[j].Schema
		}
		return allTables[i].Name < allTables[j].Name
	})

	var schemas []*core.TableSchema
	for _, t := range allTables {
		s, err := gj.GetTableSchemaForDatabaseSchema(database, t.Schema, t.Name)
		if err != nil {
			continue
		}
		schemas = append(schemas, s)
	}
	return schemas
}

func (dm *DiscoveryManager) ensureTables(database string) {
	if _, ok := dm.tablesCache.Load(database); ok {
		return
	}
	_, _, _ = dm.tablesGroup.Do(database, func() (any, error) {
		if _, ok := dm.tablesCache.Load(database); ok {
			return nil, nil
		}
		tables := dm.gj.GetTablesForDatabase(database)
		index := buildCheapTableIndex(tables)
		overview := buildCheapDatabaseOverview(database, tables, dm.gj.GetFunctionsForDatabase(database), dm.gj.EffectiveAnalyticsMode(database))
		dm.tablesCache.Store(database, &dbTablesPayload{Tables: index, DatabaseOverview: overview})
		return nil, nil
	})
}

func (dm *DiscoveryManager) ensureFullTables(database string) {
	if _, ok := dm.fullCache.Load(database); ok {
		return
	}
	_, _, _ = dm.fullGroup.Do(database, func() (any, error) {
		if _, ok := dm.fullCache.Load(database); ok {
			return nil, nil
		}
		schemas := getSchemas(dm.gj, database)
		details := buildTableDetails(schemas, nil)
		overview := buildDatabaseOverview(database, schemas, nil, dm.gj.GetFunctionsForDatabase(database), dm.gj.EffectiveAnalyticsMode(database))
		dm.fullCache.Store(database, &dbFullPayload{
			Tables:           details,
			DatabaseOverview: overview,
			Profiles:         nil,
		})
		return nil, nil
	})
}

func (dm *DiscoveryManager) ensureInsights(database string) {
	if _, ok := dm.insightsCache.Load(database); ok {
		return
	}
	_, _, _ = dm.insightsGroup.Do(database, func() (any, error) {
		if _, ok := dm.insightsCache.Load(database); ok {
			return nil, nil
		}
		schemas := getSchemas(dm.gj, database)
		insights := buildSchemaInsights(dm.gj, database, schemas, nil)
		overview := dm.DatabaseOverview(database)
		insights.DatabaseOverview = &overview
		dm.insightsCache.Store(database, &insights)
		return nil, nil
	})
}

func (dm *DiscoveryManager) Invalidate() {
	dm.tablesCache.Range(func(k, _ any) bool { dm.tablesCache.Delete(k); return true })
	dm.fullCache.Range(func(k, _ any) bool { dm.fullCache.Delete(k); return true })
	dm.insightsCache.Range(func(k, _ any) bool { dm.insightsCache.Delete(k); return true })
	dm.profileCache.Range(func(k, _ any) bool { dm.profileCache.Delete(k); return true })
	dm.namespaceCache.Range(func(k, _ any) bool { dm.namespaceCache.Delete(k); return true })
	dm.rowCountCache.Range(func(k, _ any) bool { dm.rowCountCache.Delete(k); return true })
}

// Namespaces returns the Tier-0 rollup for a database: one entry per
// (database, schema) namespace with table count and approximate row total.
// Cached per database; invalidated on schema change. One catalog query
// per cold call.
func (dm *DiscoveryManager) Namespaces(ctx context.Context, database string) []NamespaceRollup {
	if database == "" {
		var out []NamespaceRollup
		for _, name := range dm.Databases() {
			out = append(out, dm.Namespaces(ctx, name)...)
		}
		return out
	}
	if v, ok := dm.namespaceCache.Load(database); ok {
		return v.([]NamespaceRollup)
	}
	v, _, _ := dm.namespaceGroup.Do(database, func() (any, error) {
		if cached, ok := dm.namespaceCache.Load(database); ok {
			return cached, nil
		}
		rows, err := buildNamespaceRollup(ctx, dm.gj, database)
		if err != nil {
			return []NamespaceRollup(nil), nil
		}
		dm.namespaceCache.Store(database, rows)
		return rows, nil
	})
	if v == nil {
		return nil
	}
	return v.([]NamespaceRollup)
}

// RowCountsForNamespace returns table_name -> approx_row_count for every
// table in a single (database, schema) namespace. Lazy: fired only when
// the caller scopes a list_tables request to a specific schema. Cached
// per (database, schema).
func (dm *DiscoveryManager) RowCountsForNamespace(ctx context.Context, database, schema string) map[string]int64 {
	key := database + "\x00" + schema
	if v, ok := dm.rowCountCache.Load(key); ok {
		return v.(map[string]int64)
	}
	v, _, _ := dm.rowCountGroup.Do(key, func() (any, error) {
		if cached, ok := dm.rowCountCache.Load(key); ok {
			return cached, nil
		}
		counts, err := buildRowCountsForNamespace(ctx, dm.gj, database, schema)
		if err != nil {
			counts = map[string]int64{}
		}
		dm.rowCountCache.Store(key, counts)
		return counts, nil
	})
	if v == nil {
		return map[string]int64{}
	}
	return v.(map[string]int64)
}

func (dm *DiscoveryManager) TableIndex(database string) []TableIndexEntry {
	if database == "" {
		var out []TableIndexEntry
		for _, name := range dm.Databases() {
			out = append(out, dm.TableIndex(name)...)
		}
		return out
	}
	dm.ensureTables(database)
	if v, ok := dm.tablesCache.Load(database); ok {
		return v.(*dbTablesPayload).Tables
	}
	return nil
}

func (dm *DiscoveryManager) DatabaseOverview(database string) DatabaseOverview {
	dm.ensureTables(database)
	var ov DatabaseOverview
	if v, ok := dm.tablesCache.Load(database); ok {
		ov = v.(*dbTablesPayload).DatabaseOverview
	} else {
		ov = DatabaseOverview{Database: database}
	}
	dm.decorateOverviewWithRollup(&ov, database)
	return ov
}

// decorateOverviewWithRollup injects Tier-0 totals (ApproxRowTotal,
// TopNamespacesByRows, per-schema ApproxRowTotal) onto a DatabaseOverview
// without forcing eager per-table fetches.
func (dm *DiscoveryManager) decorateOverviewWithRollup(ov *DatabaseOverview, database string) {
	rollup := dm.Namespaces(context.Background(), database)
	if len(rollup) == 0 {
		return
	}
	bySchema := map[string]int64{}
	var total int64
	for _, r := range rollup {
		total += r.ApproxRowTotal
		key := r.Schema
		if key == "" {
			key = r.Database
		}
		bySchema[key] += r.ApproxRowTotal
	}
	ov.ApproxRowTotal = total

	if len(ov.Schemas) > 0 {
		for i := range ov.Schemas {
			key := ov.Schemas[i].Name
			if n, ok := bySchema[key]; ok {
				ov.Schemas[i].ApproxRowTotal = n
			}
		}
	}

	sorted := make([]NamespaceRollup, len(rollup))
	copy(sorted, rollup)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ApproxRowTotal > sorted[j].ApproxRowTotal
	})
	limit := 10
	if len(sorted) < limit {
		limit = len(sorted)
	}
	top := make([]NamespaceRollup, 0, limit)
	for i := 0; i < limit; i++ {
		if sorted[i].ApproxRowTotal == 0 && !sorted[i].RowCountAvailable {
			break
		}
		top = append(top, sorted[i])
	}
	if len(top) > 0 {
		ov.TopNamespacesByRows = top
	}
}

func (dm *DiscoveryManager) FullTables(database string) []TableDetailEntry {
	dm.ensureFullTables(database)
	if v, ok := dm.fullCache.Load(database); ok {
		return v.(*dbFullPayload).Tables
	}
	return nil
}

func (dm *DiscoveryManager) FullDatabaseOverview(database string) DatabaseOverview {
	dm.ensureFullTables(database)
	if v, ok := dm.fullCache.Load(database); ok {
		return v.(*dbFullPayload).DatabaseOverview
	}
	return DatabaseOverview{Database: database}
}

func (dm *DiscoveryManager) Insights(database string) SchemaInsights {
	dm.ensureInsights(database)
	if v, ok := dm.insightsCache.Load(database); ok {
		return *(v.(*SchemaInsights))
	}
	return SchemaInsights{Database: database}
}

func (dm *DiscoveryManager) Profile(database, table string) *TableProfile {
	key := profileCacheKey(database, "", table, "light")
	if v, ok := dm.profileCache.Load(key); ok {
		return v.(*TableSampleResult).Stats
	}
	return nil
}

func (dm *DiscoveryManager) Databases() []string {
	return dm.gj.DatabaseNames()
}

func (dm *DiscoveryManager) TableIndexPage(ctx context.Context, database string, opts TableListOptions) ListTablesResult {
	entries := dm.TableIndex(database)
	filtered := filterTableIndex(entries, opts)

	var topTables []TableRef
	if opts.Schema != "" && database != "" && len(filtered) > 0 {
		counts := dm.RowCountsForNamespace(ctx, database, opts.Schema)
		if len(counts) > 0 {
			for i := range filtered {
				if n, ok := counts[filtered[i].Name]; ok {
					filtered[i].RowCountApprox = n
				}
			}
			topTables = topTablesByRows(filtered, 10)
		}
	}

	page, nextCursor, hasMore := paginateTableIndex(filtered, opts)

	result := ListTablesResult{
		Database:        database,
		Schema:          opts.Schema,
		Tables:          page,
		Count:           len(page),
		Total:           len(filtered),
		TopTablesByRows: topTables,
		NextCursor:      nextCursor,
		HasMore:         hasMore,
	}
	if database == "" {
		result.Databases = dm.Databases()
	}
	return result
}

func topTablesByRows(entries []TableIndexEntry, limit int) []TableRef {
	type rowRef struct {
		name   string
		schema string
		rows   int64
	}
	refs := make([]rowRef, 0, len(entries))
	for _, e := range entries {
		if e.RowCountApprox > 0 {
			refs = append(refs, rowRef{e.Name, e.Schema, e.RowCountApprox})
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].rows > refs[j].rows })
	if limit > 0 && len(refs) > limit {
		refs = refs[:limit]
	}
	out := make([]TableRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, TableRef{Name: r.name, Schema: r.schema, RowCountApprox: r.rows})
	}
	return out
}

func (dm *DiscoveryManager) FullTablesPage(database string, opts TableListOptions) []TableDetailEntry {
	details := dm.FullTables(database)
	if opts.Search == "" && opts.Schema == "" && opts.Limit <= 0 && opts.Cursor == "" {
		return details
	}
	index := make([]TableIndexEntry, 0, len(details))
	for _, d := range details {
		index = append(index, d.TableIndexEntry)
	}
	filtered, _, _ := paginateTableIndex(filterTableIndex(index, opts), opts)
	allowed := make(map[string]struct{}, len(filtered))
	for _, e := range filtered {
		allowed[e.Database+":"+e.Schema+":"+e.Name] = struct{}{}
	}
	out := make([]TableDetailEntry, 0, len(filtered))
	for _, d := range details {
		key := d.Database + ":" + d.TableIndexEntry.Schema + ":" + d.Name
		if _, ok := allowed[key]; ok {
			out = append(out, d)
		}
	}
	return out
}

func filterTableIndex(entries []TableIndexEntry, opts TableListOptions) []TableIndexEntry {
	search := strings.ToLower(strings.TrimSpace(opts.Search))
	schema := strings.ToLower(strings.TrimSpace(opts.Schema))
	if search == "" && schema == "" {
		return entries
	}
	out := make([]TableIndexEntry, 0, len(entries))
	for _, e := range entries {
		if schema != "" && strings.ToLower(e.Schema) != schema {
			continue
		}
		if search != "" {
			haystack := strings.ToLower(e.Database + " " + e.Schema + " " + e.Name + " " + e.Comment)
			if !strings.Contains(haystack, search) {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

func paginateTableIndex(entries []TableIndexEntry, opts TableListOptions) ([]TableIndexEntry, string, bool) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	offset := 0
	if opts.Cursor != "" {
		if n, err := strconv.Atoi(opts.Cursor); err == nil && n > 0 {
			offset = n
		}
	}
	if offset >= len(entries) {
		return []TableIndexEntry{}, "", false
	}
	end := offset + limit
	hasMore := end < len(entries)
	if end > len(entries) {
		end = len(entries)
	}
	next := ""
	if hasMore {
		next = strconv.Itoa(end)
	}
	return entries[offset:end], next, hasMore
}

func (dm *DiscoveryManager) Payload(database string) *DiscoveryPayload {
	return &DiscoveryPayload{
		Database:         database,
		Tables:           dm.TableIndex(database),
		Insights:         dm.Insights(database),
		DatabaseOverview: dm.DatabaseOverview(database),
	}
}

func (dm *DiscoveryManager) FullPayload(database string) *DiscoveryFullPayload {
	return &DiscoveryFullPayload{
		Database:         database,
		Tables:           dm.FullTables(database),
		DatabaseOverview: dm.FullDatabaseOverview(database),
	}
}

type DiscoverySubscription struct {
	Result   chan *DiscoveryPayload
	done     chan struct{}
	database string
}

func (ds *DiscoverySubscription) Unsubscribe() {
	select {
	case <-ds.done:
	default:
		close(ds.done)
	}
}

func (dm *DiscoveryManager) Subscribe(ctx context.Context, database string) (*DiscoverySubscription, error) {
	ds := &DiscoverySubscription{
		Result:   make(chan *DiscoveryPayload, 4),
		done:     make(chan struct{}),
		database: database,
	}

	if database != "" {
		ds.Result <- dm.Payload(database)
	} else {
		for _, name := range dm.gj.DatabaseNames() {
			ds.Result <- dm.Payload(name)
		}
	}

	dm.gj.OnSchemaChange(func(dbName string, hash string) {
		select {
		case <-ds.done:
			return
		default:
		}
		if database != "" && dbName != database {
			return
		}
		payload := dm.Payload(dbName)
		select {
		case ds.Result <- payload:
		case <-ds.done:
		default:
		}
	})

	return ds, nil
}
