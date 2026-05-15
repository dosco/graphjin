package codesql

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

// Options configures a managed CodeSQL index.
type Options struct {
	// Name is the GraphJin database config key. It is used as the cache file
	// prefix under CacheDir.
	Name string
	// Root is the source tree to index.
	Root string
	// CacheDir is normally <graphjin-config>/codesql.
	CacheDir string
	// Watch starts the runtime fsnotify reconciler after the initial scan.
	Watch bool
	// InferDBRefs enables best-effort code-to-database reference extraction.
	InferDBRefs bool
	// RefTargets is an optional database metadata catalog used to resolve refs.
	RefTargets []DBRefTarget
}

// Stats describes the latest reconciliation run.
type Stats struct {
	FilesIndexed int
	FilesAdded   int
	FilesChanged int
	FilesDeleted int
	FilesSkipped int
	ParseErrors  int
	Duration     time.Duration
}

// Managed is a live SQLite-backed source-code database.
type Managed struct {
	DB        *sql.DB
	CachePath string
	Root      string

	cancel context.CancelFunc
	done   chan struct{}

	refTargets refTargetSetter
	reconciler reconciler
	notifier   sourceChangeNotifier

	lockMu      sync.Mutex
	locksLoaded bool
	locks       map[int64]*sourceLock
}

type DBRefTarget struct {
	DatabaseName string
	SchemaName   string
	TableName    string
	Columns      []string
}

type refTargetSetter interface {
	setDBRefTargets(context.Context, []DBRefTarget) error
}

type reconciler interface {
	Reconcile(context.Context) (*Stats, error)
}

type sourceChangeNotifier interface {
	setSourceChangeHook(func())
}

var managedCacheTables = []string{
	"code_languages",
	"code_grammars",
	"code_query_packs",
	"code_files",
	"code_file_versions",
	"code_index_status",
	"code_parse_errors",
	"code_nodes",
	"code_captures",
	"code_symbols",
	"code_scopes",
	"code_locals",
	"code_refs",
	"code_imports",
	"code_edges",
	"code_db_refs",
	"code_injections",
	"code_docs",
	"code_file_text_chunks",
	"code_change_sets",
	"code_locks",
	"code_change_audit",
	"gj_code",
}

// ManagedCacheTables returns the CodeSQL tables whose cached GraphQL query
// results must be invalidated after source reconciliation.
func ManagedCacheTables() []string {
	out := make([]string, len(managedCacheTables))
	copy(out, managedCacheTables)
	return out
}

func (m *Managed) SetDBRefTargets(ctx context.Context, targets []DBRefTarget) error {
	if m == nil || m.refTargets == nil {
		return nil
	}
	return m.refTargets.setDBRefTargets(ctx, targets)
}

// SetSourceChangeHook registers a callback fired after a successful reconcile
// that changes the indexed source view.
func (m *Managed) SetSourceChangeHook(fn func()) {
	if m == nil || m.notifier == nil {
		return
	}
	m.notifier.setSourceChangeHook(fn)
}

// Close stops the watcher, if any, and closes the SQLite database.
func (m *Managed) Close() error {
	if m == nil {
		return nil
	}
	if m.cancel != nil {
		m.cancel()
	}
	if m.done != nil {
		<-m.done
	}
	if m.DB != nil {
		return m.DB.Close()
	}
	return nil
}
