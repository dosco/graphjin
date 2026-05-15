package core

import (
	"errors"
	"fmt"

	"github.com/dosco/graphjin/core/v3/internal/nanodb"
)

type NanoDB = nanodb.DB
type NanoSnapshot = nanodb.Snapshot
type NanoTable = nanodb.Table
type NanoColumn = nanodb.Column
type NanoRow = nanodb.Row

func NewNanoDB(snapshot NanoSnapshot) (*NanoDB, error) {
	return nanodb.New(snapshot)
}

func OptionSetNanoDatabases(databases map[string]*NanoDB) Option {
	return func(gj *graphjinEngine) error {
		if gj.databases == nil {
			gj.databases = make(map[string]*dbContext)
		}
		for name, db := range databases {
			if db == nil {
				return errors.New("nanodb option contains nil database")
			}
			snap := db.Snapshot()
			if snap == nil {
				return fmt.Errorf("nanodb database %q has no snapshot", name)
			}
			gj.databases[name] = &dbContext{
				name:   name,
				dbtype: "nanodb",
				dbinfo: snap.DBInfo(name),
				nano:   db,
			}
			if gj.conf.Databases == nil {
				gj.conf.Databases = make(map[string]DatabaseConfig)
			}
			if _, ok := gj.conf.Databases[name]; !ok {
				gj.conf.Databases[name] = DatabaseConfig{Type: "nanodb", ReadOnly: true}
			}
		}
		return nil
	}
}
