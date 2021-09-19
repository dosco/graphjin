package core

import (
	"fmt"
	"time"

	"github.com/dosco/graphjin/core/internal/sdata"
)

func (g *GraphJin) initDBWatcher() error {
	var err error
	go func() {
		err = g.startDBWatcher()
	}()
	if err != nil {
		return fmt.Errorf("error in database watcher: %w", err)
	}
	return nil
}

func (g *GraphJin) startDBWatcher() error {
	var ps time.Duration
	gj := g.Load().(*graphjin)

	switch d := gj.conf.DBSchemaPollDuration; {
	case d == 0 && g.IsProd():
		return nil
	case d < 5:
		ps = 10 * time.Second
	default:
		ps = d * time.Second
	}

	ticker := time.NewTicker(ps)
	defer ticker.Stop()

	for range ticker.C {
		gj := g.Load().(*graphjin)
		dbinfo, err := sdata.GetDBInfo(
			gj.db,
			gj.dbtype,
			gj.conf.Blocklist)

		if err != nil {
			return err
		}

		if dbinfo.Hash() != gj.dbinfo.Hash() {
			gj.log.Println("database change detected. reinitializing...")
			if err = g.Reload(); err != nil {
				return err
			}
		}
	}

	return nil
}
