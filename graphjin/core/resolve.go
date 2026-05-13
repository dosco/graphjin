package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

type ResolverFn func(v ResolverProps) (Resolver, error)

type resItem struct {
	IDField     []byte
	Path        [][]byte
	Fn          Resolver
	Source      string
	Scope       string
	Fingerprint string
}

// newRTMap returns a map of resolver functions
func (gj *graphjinEngine) newRTMap() map[string]ResolverFn {
	return map[string]ResolverFn{
		"remote_api": func(v ResolverProps) (Resolver, error) {
			return newRemoteAPI(v, gj.trace.NewHTTPClient())
		},
		// "openapi" wraps an openapi.Caller resolved at request time
		// from gj.openapiRuntime. The factory closes over gj so the
		// runtime reference doesn't have to ride on every Props bag.
		"openapi": gj.newOpenAPIResolverFn(),
		// "filesystem" routes a synthesised remote table to one of the
		// configured fstable.Backend instances by name.
		"filesystem": gj.newFilesystemResolverFn(),
	}
}

// initResolvers initializes the resolvers
func (gj *graphjinEngine) initResolvers() error {
	gj.rmap = make(map[string]resItem)

	// Load OpenAPI integration before the rtmap is built so the
	// "openapi" factory in newRTMap can see gj.openapiRuntime, and
	// before the resolver loop so any synthesised ResolverConfigs are
	// processed in the same pass as user-declared ones.
	if err := gj.loadOpenAPIIntegration(); err != nil {
		return err
	}

	// Filesystem virtual tables: install the built-in "local" factory
	// before the bridge loads so locally-declared filesystems work
	// out-of-the-box; serv layer adds s3/gcs via OptionSetFilesystemBackend.
	gj.registerBuiltinFilesystemFactories()
	if err := gj.loadFilesystemIntegration(); err != nil {
		return err
	}

	if gj.rtmap == nil {
		gj.rtmap = gj.newRTMap()
	}

	pdb := gj.primaryDB()
	if pdb == nil || pdb.dbinfo == nil {
		return nil
	}

	for i, r := range gj.conf.Resolvers {
		if r.Schema == "" {
			gj.conf.Resolvers[i].Schema = pdb.dbinfo.Schema
			r.Schema = pdb.dbinfo.Schema
		}
		if err := gj.initRemote(r, gj.rtmap, pdb.dbinfo); err != nil {
			return fmt.Errorf("resolvers: %w", err)
		}
	}
	return nil
}

// initRemote initializes the remote resolver
func (gj *graphjinEngine) initRemote(
	rc ResolverConfig, rtmap map[string]ResolverFn, dbinfo *sdata.DBInfo,
) error {
	if rc.Table == "" {
		return gj.initToplevelRemote(rc, rtmap, dbinfo)
	}

	// Defines the table column to be used as an id in the
	// remote reques
	var col sdata.DBColumn

	ti, err := dbinfo.GetTable(rc.Schema, rc.Table)
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
	nt.PrimaryCols = []sdata.DBColumn{col1}
	nt.PrimaryCol = col1

	dbinfo.AddTable(nt)

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
		IDField:     []byte(idk),
		Path:        path,
		Fn:          fn,
		Source:      rc.Type,
		Scope:       rc.Name,
		Fingerprint: resolverConfigFingerprint(rc),
	}

	// Index resolver obj by parent and child names
	gj.rmap[(rc.Name + rc.Table)] = rf

	// Index resolver obj by IDField
	gj.rmap[string(rf.IDField)] = rf

	return nil
}

// initToplevelRemote registers a parent-less remote resolver. The synthetic
// table is pre-registered in dbinfo by the bridge during loadOpenAPIIntegration.
func (gj *graphjinEngine) initToplevelRemote(
	rc ResolverConfig, rtmap map[string]ResolverFn, dbinfo *sdata.DBInfo,
) error {
	if _, err := dbinfo.GetTable(rc.Schema, rc.Name); err != nil {
		return fmt.Errorf("top-level remote %q: synthetic table not registered: %w", rc.Name, err)
	}

	factory, ok := rtmap[rc.Type]
	if !ok {
		return fmt.Errorf("unknown resolver type: %s", rc.Type)
	}
	fn, err := factory(rc.Props)
	if err != nil {
		return err
	}

	var path [][]byte
	if rc.StripPath != "" {
		for _, p := range strings.Split(rc.StripPath, ".") {
			path = append(path, []byte(p))
		}
	}

	gj.rmap[rc.Name] = resItem{Path: path, Fn: fn, Source: rc.Type, Scope: rc.Name, Fingerprint: resolverConfigFingerprint(rc)}
	return nil
}

func resolverConfigFingerprint(rc ResolverConfig) string {
	b, err := json.Marshal(rc)
	if err != nil {
		b = []byte(fmt.Sprintf("%s|%s|%s|%s|%s|%s", rc.Name, rc.Type, rc.Schema, rc.Table, rc.Column, rc.StripPath))
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
