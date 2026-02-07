package core

import (
	"time"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// initDBWatcher initializes the database schema watcher
func (g *GraphJin) initDBWatcher() error {
	gj := g.Load().(*graphjinEngine)

	// no schema polling in production
	if gj.prod {
		return nil
	}

	ps := gj.conf.DBSchemaPollDuration

	switch {
	case ps < (1 * time.Second):
		return nil

	case ps < (5 * time.Second):
		ps = 10 * time.Second
	}

	go func() {
		g.startDBWatcher(ps)
	}()
	return nil
}

// startDBWatcher starts the database schema watcher
func (g *GraphJin) startDBWatcher(ps time.Duration) {
	ticker := time.NewTicker(ps)
	defer ticker.Stop()

	for range ticker.C {
		gj := g.Load().(*graphjinEngine)

		latestDi, err := sdata.GetDBInfo(
			gj.db,
			gj.dbtype,
			gj.conf.Blocklist)
		if err != nil {
			gj.log.Println(err)
			continue
		}

		// Check if we're waiting for tables (schema is nil)
		if gj.schema == nil {
			if len(latestDi.Tables) > 0 {
				g.reloadMu.Lock()
				gj = g.Load().(*graphjinEngine)
				if gj.schema == nil {
					gj.log.Println("tables discovered, initializing schema...")
					if err := g.newGraphJin(gj.conf, gj.db, latestDi, gj.fs, gj.opts...); err != nil {
						gj.log.Println(err)
					}
				}
				g.reloadMu.Unlock()
			}
			// Continue polling - don't check hash when waiting for tables
			continue
		}

		// Normal operation - check for schema changes
		if latestDi.Hash() == gj.dbinfo.Hash() {
			continue
		}

		g.reloadMu.Lock()
		// Re-check after lock — another reload may have already updated the engine
		gj = g.Load().(*graphjinEngine)
		if latestDi.Hash() != gj.dbinfo.Hash() {
			gj.log.Println("database change detected. reinitializing...")
			if err := g.newGraphJin(gj.conf, gj.db, latestDi, gj.fs, gj.opts...); err != nil {
				gj.log.Println(err)
			}
		}
		g.reloadMu.Unlock()

		select {
		case <-g.done:
			return
		default:
		}
	}
}
