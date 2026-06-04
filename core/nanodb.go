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

type NanoUpdate struct {
	inner *nanodb.Update
}

func NewNanoDB(snapshot NanoSnapshot) (*NanoDB, error) {
	return nanodb.New(snapshot)
}

func UpdateNanoDB(db *NanoDB, fn func(*NanoUpdate) error) error {
	if db == nil {
		return errors.New("nanodb is nil")
	}
	if fn == nil {
		return errors.New("nanodb update callback is required")
	}
	return db.Update(func(tx *nanodb.Update) error {
		return fn(&NanoUpdate{inner: tx})
	})
}

func (tx *NanoUpdate) ReplaceRows(schema, table string, remove func(NanoRow) bool, insert []NanoRow) error {
	if tx == nil || tx.inner == nil {
		return errors.New("nanodb update is nil")
	}
	return tx.inner.ReplaceRows(schema, table, remove, insert)
}

func (tx *NanoUpdate) ReplaceTable(schema, table string, rows []NanoRow) error {
	if tx == nil || tx.inner == nil {
		return errors.New("nanodb update is nil")
	}
	return tx.inner.ReplaceTable(schema, table, rows)
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
