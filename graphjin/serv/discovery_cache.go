package serv

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	core "github.com/dosco/graphjin/core/v3"
)

// DiscoveryManager manages cached discovery sections and subscriptions.
// Each section (syntax, tables, full_tables, insights) generates independently
// on first access. Only full_tables runs enrichment queries — the other
// sections return instantly from in-memory schema metadata.
type DiscoveryManager struct {
	gj *core.GraphJin

	// Per-section lazy gates. Uses mutex + bool instead of sync.Once
	// because sync.Once cannot be safely reset concurrently.
	mu           sync.Mutex
	syntaxDone   bool
	tablesDone   bool
	fullDone     bool
	insightsDone bool

	// Cached section content: "dbname" → section string
	syntaxCache   sync.Map
	tablesCache   sync.Map
	fullCache     sync.Map
	insightsCache sync.Map
}

// NewDiscoveryManager creates a DiscoveryManager and registers an OnSchemaChange
// callback so caches are invalidated whenever any database schema changes.
func NewDiscoveryManager(gj *core.GraphJin) *DiscoveryManager {
	dm := &DiscoveryManager{gj: gj}
	gj.OnSchemaChange(func(dbName, hash string) {
		dm.invalidate()
	})
	return dm
}

// getSchemas returns sorted table schemas for a database.
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

func (dm *DiscoveryManager) ensureSyntax() {
	dm.mu.Lock()
	if dm.syntaxDone {
		dm.mu.Unlock()
		return
	}
	dm.mu.Unlock()

	names := dm.gj.DatabaseNames()
	if len(names) == 0 {
		return
	}
	content := generateSyntax()
	for _, name := range names {
		dm.syntaxCache.Store(name, content)
	}

	dm.mu.Lock()
	dm.syntaxDone = true
	dm.mu.Unlock()
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
	for _, name := range names {
		schemas := getSchemas(dm.gj, name)
		dm.tablesCache.Store(name, generateTablesCompact(dm.gj, schemas))
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
		dm.fullCache.Store(name, generateTablesFull(ctx, dm.gj, name, schemas))
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
		dm.insightsCache.Store(name, generateInsights(dm.gj, name, schemas))
	}

	dm.mu.Lock()
	dm.insightsDone = true
	dm.mu.Unlock()
}

// invalidate clears all caches and resets all generation flags.
func (dm *DiscoveryManager) invalidate() {
	dm.mu.Lock()
	dm.syntaxDone = false
	dm.tablesDone = false
	dm.fullDone = false
	dm.insightsDone = false
	dm.mu.Unlock()

	dm.syntaxCache.Range(func(k, _ any) bool { dm.syntaxCache.Delete(k); return true })
	dm.tablesCache.Range(func(k, _ any) bool { dm.tablesCache.Delete(k); return true })
	dm.fullCache.Range(func(k, _ any) bool { dm.fullCache.Delete(k); return true })
	dm.insightsCache.Range(func(k, _ any) bool { dm.insightsCache.Delete(k); return true })
}

// Get returns a DiscoveryDocument assembled from cached sections.
// Triggers syntax, tables, and insights (NOT full_tables).
func (dm *DiscoveryManager) Get(database string) *DiscoveryDocument {
	dm.ensureSyntax()
	dm.ensureTables()
	dm.ensureInsights()

	doc := &DiscoveryDocument{
		Database:    database,
		Hash:        fmt.Sprintf("%x", time.Now().UnixNano()),
		GeneratedAt: time.Now().UTC(),
	}
	if v, ok := dm.syntaxCache.Load(database); ok {
		doc.Syntax = v.(string)
	}
	if v, ok := dm.tablesCache.Load(database); ok {
		doc.Tables = v.(string)
	}
	if v, ok := dm.insightsCache.Load(database); ok {
		doc.Insights = v.(string)
	}
	return doc
}

// GetAll returns discovery documents for every known database.
func (dm *DiscoveryManager) GetAll() []*DiscoveryDocument {
	var docs []*DiscoveryDocument
	for _, name := range dm.gj.DatabaseNames() {
		docs = append(docs, dm.Get(name))
	}
	return docs
}

// CombinedSection returns a single section concatenated across all databases.
// Only triggers generation of the requested section.
func (dm *DiscoveryManager) CombinedSection(section string) string {
	var cache *sync.Map
	switch section {
	case "syntax":
		dm.ensureSyntax()
		cache = &dm.syntaxCache
	case "tables":
		dm.ensureTables()
		cache = &dm.tablesCache
	case "full_tables":
		dm.ensureFullTables()
		cache = &dm.fullCache
	case "insights":
		dm.ensureInsights()
		cache = &dm.insightsCache
	default:
		return ""
	}

	var sb strings.Builder
	for _, name := range dm.gj.DatabaseNames() {
		if v, ok := cache.Load(name); ok {
			sb.WriteString(v.(string))
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// Combined returns syntax + tables + insights for every database.
// Does NOT include full_tables.
func (dm *DiscoveryManager) Combined() string {
	dm.ensureSyntax()
	dm.ensureTables()
	dm.ensureInsights()

	var sb strings.Builder
	for _, name := range dm.gj.DatabaseNames() {
		if v, ok := dm.syntaxCache.Load(name); ok {
			sb.WriteString(v.(string))
		}
		if v, ok := dm.tablesCache.Load(name); ok {
			sb.WriteString(v.(string))
		}
		if v, ok := dm.insightsCache.Load(name); ok {
			sb.WriteString(v.(string))
		}
	}
	return sb.String()
}

// DiscoverySubscription represents a live subscription to discovery document
// updates. Callers receive new documents on Result whenever the schema changes.
type DiscoverySubscription struct {
	Result   chan *DiscoveryDocument
	done     chan struct{}
	database string
}

// Unsubscribe terminates the subscription and stops future deliveries.
func (ds *DiscoverySubscription) Unsubscribe() {
	select {
	case <-ds.done:
	default:
		close(ds.done)
	}
}

// Subscribe creates a live subscription for discovery document updates.
// Initial documents are sent immediately (no full_tables).
func (dm *DiscoveryManager) Subscribe(ctx context.Context, database string) (*DiscoverySubscription, error) {
	ds := &DiscoverySubscription{
		Result:   make(chan *DiscoveryDocument, 4),
		done:     make(chan struct{}),
		database: database,
	}

	if database != "" {
		ds.Result <- dm.Get(database)
	} else {
		for _, name := range dm.gj.DatabaseNames() {
			ds.Result <- dm.Get(name)
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
		doc := dm.Get(dbName)
		select {
		case ds.Result <- doc:
		case <-ds.done:
		default:
		}
	})

	return ds, nil
}
