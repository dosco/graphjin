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

		needsReload := false

		// Check all databases for schema changes
		for _, ctx := range gj.databases {
			if ctx.db == nil {
				continue
			}

			latestDi, err := sdata.GetDBInfo(
				ctx.db,
				ctx.dbtype,
				gj.conf.Blocklist)
			if err != nil {
				gj.log.Printf("database %s: schema poll error: %v", ctx.name, err)
				continue
			}

			// Check if we're waiting for tables (schema is nil)
			if ctx.schema == nil {
				if len(latestDi.Tables) > 0 {
					gj.log.Printf("database %s: tables discovered, reinitializing...", ctx.name)
					needsReload = true
					break
				}
				continue
			}

			// Normal operation - check for schema changes
			if latestDi.Hash() != ctx.dbinfo.Hash() {
				gj.log.Printf("database %s: schema change detected, reinitializing...", ctx.name)
				needsReload = true
				break
			}
		}

		if needsReload {
			g.reloadMu.Lock()
			// Re-check after lock — another reload may have already updated the engine
			gj = g.Load().(*graphjinEngine)
			pdb := gj.primaryDB()
			if pdb != nil {
				if err := g.newGraphJin(gj.conf, pdb.db, nil, gj.fs, gj.opts...); err != nil {
					gj.log.Println(err)
				}
			}
			g.reloadMu.Unlock()
		}

		select {
		case <-g.done:
			return
		default:
		}
	}
}
