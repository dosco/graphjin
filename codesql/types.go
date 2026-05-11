package codesql

import (
	"context"
	"database/sql"
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
