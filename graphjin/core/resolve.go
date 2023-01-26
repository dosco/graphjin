package core

import (
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

type ResolverFn func(v ResolverProps) (Resolver, error)

type resItem struct {
	IDField []byte
	Path    [][]byte
	Fn      Resolver
}

func (gj *graphjin) newRTMap() map[string]ResolverFn {
	return map[string]ResolverFn{
		"remote_api": func(v ResolverProps) (Resolver, error) {
			return newRemoteAPI(v, gj.trace.NewHTTPClient())
		},
	}
}

func (gj *graphjin) initResolvers() error {
	gj.rmap = make(map[string]resItem)

	if gj.rtmap == nil {
		gj.rtmap = gj.newRTMap()
	}

	for i, r := range gj.conf.Resolvers {
		if r.Schema == "" {
			gj.conf.Resolvers[i].Schema = gj.dbinfo.Schema
			r.Schema = gj.dbinfo.Schema
		}
		if err := gj.initRemote(r, gj.rtmap); err != nil {
			return fmt.Errorf("resolvers: %w", err)
		}
	}
	return nil
}

func (gj *graphjin) initRemote(
	rc ResolverConfig, rtmap map[string]ResolverFn,
) error {
	// Defines the table column to be used as an id in the
	// remote reques
	var col sdata.DBColumn

	ti, err := gj.dbinfo.GetTable(rc.Schema, rc.Table)
	if err != nil {
		return err
	}

	// If no table column specified in the config then
	// use the primary key of the table as the id
	if rc.Column != "" {
		idcol, err := ti.GetColumn(rc.Column)
		if err != nil {
			return err
		}
		col = idcol
	} else {
		col = ti.PrimaryCol
	}

	idk := fmt.Sprintf("__%s_%s", rc.Name, col.Name)
	col1 := sdata.DBColumn{
		PrimaryKey: true,
		Schema:     rc.Schema,
		Table:      rc.Name,
		Name:       idk,
		Type:       col.Type,
		FKeySchema: col.Schema,
		FKeyTable:  col.Table,
		FKeyCol:    col.Name,
	}

	nt := sdata.NewDBTable(rc.Schema, rc.Name, "remote", nil)
	nt.PrimaryCol = col1

	gj.dbinfo.AddTable(nt)

	// The function thats called to resolve this remote
	// data request
	var fn Resolver

	if v, ok := rtmap[rc.Type]; ok {
		fn, err = v(rc.Props)
	} else {
		err = fmt.Errorf("unknown resolver type: %s", rc.Type)
	}

	if err != nil {
		return err
	}

	path := [][]byte{}
	for _, p := range strings.Split(rc.StripPath, ".") {
		path = append(path, []byte(p))
	}

	rf := resItem{
		IDField: []byte(idk),
		Path:    path,
		Fn:      fn,
	}

	// Index resolver obj by parent and child names
	gj.rmap[(rc.Name + rc.Table)] = rf

	// Index resolver obj by IDField
	gj.rmap[string(rf.IDField)] = rf

	return nil
}
