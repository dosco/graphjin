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

		if latestDi.Hash() == gj.dbinfo.Hash() {
			continue
		}

		gj.log.Println("database change detected. reinitializing...")

		if err := g.reload(latestDi); err != nil {
			gj.log.Println(err)
		}

		select {
		case <-g.done:
			return
		default:
		}
	}
}
