package core

import (
	"time"

	"github.com/dosco/graphjin/core/internal/sdata"
)

func (g *GraphJin) initDBWatcher() error {
	gj := g.Load().(*graphjin)

	// no schema polling in production unless allowlist is disabled
	if g.IsProd() && !gj.conf.DisableAllowList {
		return nil
	}

	var ps time.Duration

	switch d := gj.conf.DBSchemaPollDuration; {
	case d < 0:
		return nil
	case d < 5:
		ps = 10 * time.Second
	default:
		ps = d * time.Second
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
