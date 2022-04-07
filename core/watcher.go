package core

import (
	"time"

	"github.com/dosco/graphjin/core/internal/sdata"
)

func (g *GraphJin) initDBWatcher() error {
	gj := g.Load().(*graphjin)

	// no schema polling in production unless allowlist is disabled
	if gj.prod && !gj.conf.DisableAllowList {
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

func (g *GraphJin) startDBWatcher(ps time.Duration) {
	ticker := time.NewTicker(ps)
	defer ticker.Stop()

	for range ticker.C {
		gj := g.Load().(*graphjin)

		dbinfo, err := sdata.GetDBInfo(
			gj.db,
			gj.dbtype,
			gj.conf.Blocklist)

		if err != nil {
			gj.log.Println(err)
			continue
		}

		if dbinfo.Hash() == gj.dbinfo.Hash() {
			continue
		}

		gj.log.Println("database change detected. reinitializing...")

		if err := g.Reload(); err != nil {
			gj.log.Println(err)
		}
	}
}
