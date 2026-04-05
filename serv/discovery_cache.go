package serv

import (
	"context"
	"strings"
	"sync"

	core "github.com/dosco/graphjin/core/v3"
)

// DiscoveryManager manages cached discovery documents and subscriptions.
// It lazily generates documents on first access and invalidates on schema changes.
type DiscoveryManager struct {
	gj    *core.GraphJin
	cache sync.Map // map[string]*DiscoveryDocument
	once  sync.Once
}

// newDiscoveryManager creates a discoveryManager and registers an OnSchemaChange
// callback so the cache is invalidated whenever any database schema changes.
func NewDiscoveryManager(gj *core.GraphJin) *DiscoveryManager {
	dm := &DiscoveryManager{gj: gj}
	gj.OnSchemaChange(func(dbName, hash string) {
		dm.invalidate()
	})
	return dm
}

// ensureDiscovery lazily generates discovery documents for all databases on
// first access. Subsequent calls are no-ops until invalidate resets the once.
// If the engine is not ready yet (DatabaseNames returns empty), the once is
// reset so the next call retries rather than permanently caching nothing.
func (dm *DiscoveryManager) EnsureDiscovery() {
	dm.once.Do(func() {
		names := dm.gj.DatabaseNames()
		if len(names) == 0 {
			// Engine not initialized yet — reset so next call retries.
			dm.once = sync.Once{}
			return
		}
		ctx := context.Background()
		for _, name := range names {
			doc, err := generateDiscovery(ctx, dm.gj, name)
			if err != nil {
				continue
			}
			dm.cache.Store(name, doc)
		}
	})
}

// invalidate clears all cached documents and resets the once gate so the next
// access triggers a fresh generation pass.
func (dm *DiscoveryManager) invalidate() {
	dm.cache.Range(func(key, _ any) bool {
		dm.cache.Delete(key)
		return true
	})
	dm.once = sync.Once{}
}

// Get returns the cached discovery document for a single database, or nil if
// the database is unknown.
func (dm *DiscoveryManager) Get(database string) *DiscoveryDocument {
	dm.EnsureDiscovery()
	if v, ok := dm.cache.Load(database); ok {
		return v.(*DiscoveryDocument)
	}
	return nil
}

// GetAll returns discovery documents for every known database.
func (dm *DiscoveryManager) GetAll() []*DiscoveryDocument {
	dm.EnsureDiscovery()
	var docs []*DiscoveryDocument
	dm.cache.Range(func(key, value any) bool {
		docs = append(docs, value.(*DiscoveryDocument))
		return true
	})
	return docs
}

// CombinedSection returns a single section concatenated across all databases.
// Valid sections: "syntax", "tables", "full_tables", "insights".
func (dm *DiscoveryManager) CombinedSection(section string) string {
	dm.EnsureDiscovery()
	var sb strings.Builder
	for _, name := range dm.gj.DatabaseNames() {
		if v, ok := dm.cache.Load(name); ok {
			doc := v.(*DiscoveryDocument)
			switch section {
			case "syntax":
				sb.WriteString(doc.Syntax)
			case "tables":
				sb.WriteString(doc.Tables)
			case "full_tables":
				sb.WriteString(doc.FullTables)
			case "insights":
				sb.WriteString(doc.Insights)
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// Combined returns all sections (syntax, tables, insights) concatenated for
// every database. Prefer CombinedSection for loading individual sections.
func (dm *DiscoveryManager) Combined() string {
	dm.EnsureDiscovery()
	var sb strings.Builder
	for _, name := range dm.gj.DatabaseNames() {
		if v, ok := dm.cache.Load(name); ok {
			doc := v.(*DiscoveryDocument)
			sb.WriteString(doc.Syntax)
			sb.WriteString(doc.Tables)
			sb.WriteString(doc.Insights)
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
// If database is non-empty only that database is tracked; otherwise all
// databases are included. The initial document(s) are sent immediately.
func (dm *DiscoveryManager) Subscribe(ctx context.Context, database string) (*DiscoverySubscription, error) {
	ds := &DiscoverySubscription{
		Result:   make(chan *DiscoveryDocument, 4),
		done:     make(chan struct{}),
		database: database,
	}

	// Send initial document(s).
	if database != "" {
		doc, err := generateDiscovery(ctx, dm.gj, database)
		if err != nil {
			return nil, err
		}
		ds.Result <- doc
	} else {
		for _, name := range dm.gj.DatabaseNames() {
			doc, err := generateDiscovery(ctx, dm.gj, name)
			if err != nil {
				continue
			}
			ds.Result <- doc
		}
	}

	// Register callback for future schema changes.
	dm.gj.OnSchemaChange(func(dbName string, hash string) {
		select {
		case <-ds.done:
			return
		default:
		}

		if database != "" && dbName != database {
			return
		}

		doc, err := generateDiscovery(ctx, dm.gj, dbName)
		if err != nil {
			return
		}

		select {
		case ds.Result <- doc:
		case <-ds.done:
		default:
		}
	})

	return ds, nil
}
