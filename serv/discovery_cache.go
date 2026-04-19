package serv

import (
	"context"
	"sort"
	"sync"

	core "github.com/dosco/graphjin/core/v3"
)

type DiscoveryManager struct {
	gj *core.GraphJin

	mu           sync.Mutex
	tablesDone   bool
	fullDone     bool
	insightsDone bool

	tablesCache   sync.Map
	fullCache     sync.Map
	insightsCache sync.Map
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
		dm.invalidate()
	})
	return dm
}

func getSchemas(gj *core.GraphJin, database string) []*core.TableSchema {
	allTables := gj.GetTablesForDatabase(database)
	sort.Slice(allTables, func(i, j int) bool { return allTables[i].Name < allTables[j].Name })

	var schemas []*core.TableSchema
	for _, t := range allTables {
		s, err := gj.GetTableSchemaForDatabase(database, t.Name)
		if err != nil {
			continue
		}
		schemas = append(schemas, s)
	}
	return schemas
}

func (dm *DiscoveryManager) ensureTables() {
	dm.mu.Lock()
	if dm.tablesDone {
		dm.mu.Unlock()
		return
	}
	dm.mu.Unlock()

	names := dm.gj.DatabaseNames()
	if len(names) == 0 {
		return
	}
	ctx := context.Background()
	for _, name := range names {
		schemas := getSchemas(dm.gj, name)
		rowCounts := buildRowCounts(ctx, dm.gj, name, schemas)
		profiles := profilesFromRowCounts(rowCounts)
		index := buildTableIndex(schemas, profiles)
		overview := buildDatabaseOverview(name, schemas, profiles, dm.gj.GetFunctionsForDatabase(name))
		dm.tablesCache.Store(name, &dbTablesPayload{Tables: index, DatabaseOverview: overview})
	}

	dm.mu.Lock()
	dm.tablesDone = true
	dm.mu.Unlock()
}

func (dm *DiscoveryManager) ensureFullTables() {
	dm.mu.Lock()
	if dm.fullDone {
		dm.mu.Unlock()
		return
	}
	dm.mu.Unlock()

	names := dm.gj.DatabaseNames()
	if len(names) == 0 {
		return
	}
	ctx := context.Background()
	for _, name := range names {
		schemas := getSchemas(dm.gj, name)
		profiles := buildEnrichment(ctx, dm.gj, name, schemas)
		details := buildTableDetails(schemas, profiles)
		overview := buildDatabaseOverview(name, schemas, profiles, dm.gj.GetFunctionsForDatabase(name))
		dm.fullCache.Store(name, &dbFullPayload{
			Tables:           details,
			DatabaseOverview: overview,
			Profiles:         profiles,
		})
	}

	dm.mu.Lock()
	dm.fullDone = true
	dm.mu.Unlock()
}

func (dm *DiscoveryManager) ensureInsights() {
	dm.mu.Lock()
	if dm.insightsDone {
		dm.mu.Unlock()
		return
	}
	dm.mu.Unlock()

	names := dm.gj.DatabaseNames()
	if len(names) == 0 {
		return
	}
	for _, name := range names {
		schemas := getSchemas(dm.gj, name)
		insights := buildSchemaInsights(dm.gj, name, schemas, nil)
		dm.insightsCache.Store(name, &insights)
	}

	dm.mu.Lock()
	dm.insightsDone = true
	dm.mu.Unlock()
}

func (dm *DiscoveryManager) invalidate() {
	dm.mu.Lock()
	dm.tablesDone = false
	dm.fullDone = false
	dm.insightsDone = false
	dm.mu.Unlock()

	dm.tablesCache.Range(func(k, _ any) bool { dm.tablesCache.Delete(k); return true })
	dm.fullCache.Range(func(k, _ any) bool { dm.fullCache.Delete(k); return true })
	dm.insightsCache.Range(func(k, _ any) bool { dm.insightsCache.Delete(k); return true })
}

func (dm *DiscoveryManager) TableIndex(database string) []TableIndexEntry {
	dm.ensureTables()
	if v, ok := dm.tablesCache.Load(database); ok {
		return v.(*dbTablesPayload).Tables
	}
	return nil
}

func (dm *DiscoveryManager) DatabaseOverview(database string) DatabaseOverview {
	dm.ensureTables()
	if v, ok := dm.tablesCache.Load(database); ok {
		return v.(*dbTablesPayload).DatabaseOverview
	}
	return DatabaseOverview{Database: database}
}

func (dm *DiscoveryManager) FullTables(database string) []TableDetailEntry {
	dm.ensureFullTables()
	if v, ok := dm.fullCache.Load(database); ok {
		return v.(*dbFullPayload).Tables
	}
	return nil
}

func (dm *DiscoveryManager) FullDatabaseOverview(database string) DatabaseOverview {
	dm.ensureFullTables()
	if v, ok := dm.fullCache.Load(database); ok {
		return v.(*dbFullPayload).DatabaseOverview
	}
	return DatabaseOverview{Database: database}
}

func (dm *DiscoveryManager) Insights(database string) SchemaInsights {
	dm.ensureInsights()
	if v, ok := dm.insightsCache.Load(database); ok {
		return *(v.(*SchemaInsights))
	}
	return SchemaInsights{Database: database}
}

func (dm *DiscoveryManager) Profile(database, table string) *TableProfile {
	dm.ensureFullTables()
	if v, ok := dm.fullCache.Load(database); ok {
		if p, ok := v.(*dbFullPayload).Profiles[table]; ok {
			return p
		}
	}
	return nil
}

func (dm *DiscoveryManager) Databases() []string {
	return dm.gj.DatabaseNames()
}

func profilesFromRowCounts(counts map[string]int64) map[string]*TableProfile {
	if len(counts) == 0 {
		return nil
	}
	out := make(map[string]*TableProfile, len(counts))
	for name, n := range counts {
		out[name] = &TableProfile{RowCountApprox: n}
	}
	return out
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
