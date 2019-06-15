package serv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/dosco/super-graph/jsn"
	"github.com/dosco/super-graph/qcode"
	"github.com/go-pg/pg"
	"github.com/valyala/fasttemplate"
)

const (
	empty = ""
)

// var (
// 	cache, _ = bigcache.NewBigCache(bigcache.DefaultConfig(24 * time.Hour))
// )

type coreContext struct {
	req gqlReq
	res gqlResp
	context.Context
}

func (c *coreContext) handleReq(w io.Writer, req *http.Request) error {
	var err error

	qc, err := qcompile.CompileQuery([]byte(c.req.Query))
	if err != nil {
		return err
	}

	vars := varMap(c)

	data, skipped, err := c.resolveSQL(qc, vars)
	if err != nil {
		return err
	}

	if len(data) == 0 || skipped == 0 {
		return c.render(w, data)
	}

	sel := qc.Query.Selects
	h := xxhash.New()

	// fetch the field name used within the db response json
	// that are used to mark insertion points and the mapping between
	// those field names and their select objects
	fids, sfmap := parentFieldIds(h, sel, skipped)

	// fetch the field values of the marked insertion points
	// these values contain the id to be used with fetching remote data
	from := jsn.Get(data, fids)

	var to []jsn.Field
	switch {
	case len(from) == 1:
		to, err = c.resolveRemote(req, h, from[0], sel, sfmap)

	case len(from) > 1:
		to, err = c.resolveRemotes(req, h, from, sel, sfmap)

	default:
		return errors.New("something wrong no remote ids found in db response")
	}

	if err != nil {
		return err
	}

	var ob bytes.Buffer

	err = jsn.Replace(&ob, data, from, to)
	if err != nil {
		return err
	}

	return c.render(w, ob.Bytes())
}

func (c *coreContext) resolveRemote(
	req *http.Request,
	h *xxhash.Digest,
	field jsn.Field,
	sel []qcode.Select,
	sfmap map[uint64]*qcode.Select) ([]jsn.Field, error) {

	// replacement data for the marked insertion points
	// key and value will be replaced by whats below
	toA := [1]jsn.Field{}
	to := toA[:1]

	// use the json key to find the related Select object
	k1 := xxhash.Sum64(field.Key)

	s, ok := sfmap[k1]
	if !ok {
		return nil, nil
	}
	p := sel[s.ParentID]

	// then use the Table nme in the Select and it's parent
	// to find the resolver to use for this relationship
	k2 := mkkey(h, s.Table, p.Table)

	r, ok := rmap[k2]
	if !ok {
		return nil, nil
	}

	id := jsn.Value(field.Value)
	if len(id) == 0 {
		return nil, nil
	}

	st := time.Now()

	b, err := r.Fn(req, id)
	if err != nil {
		return nil, err
	}

	if conf.EnableTracing {
		c.addTrace(sel, s.ID, st)
	}

	if len(r.Path) != 0 {
		b = jsn.Strip(b, r.Path)
	}

	var ob bytes.Buffer

	if len(s.Cols) != 0 {
		err = jsn.Filter(&ob, b, colsToList(s.Cols))
		if err != nil {
			return nil, err
		}

	} else {
		ob.WriteString("null")
	}

	to[0] = jsn.Field{[]byte(s.FieldName), ob.Bytes()}
	return to, nil
}

func (c *coreContext) resolveRemotes(
	req *http.Request,
	h *xxhash.Digest,
	from []jsn.Field,
	sel []qcode.Select,
	sfmap map[uint64]*qcode.Select) ([]jsn.Field, error) {

	// replacement data for the marked insertion points
	// key and value will be replaced by whats below
	to := make([]jsn.Field, len(from))

	var wg sync.WaitGroup
	wg.Add(len(from))

	var cerr error

	for i, id := range from {

		// use the json key to find the related Select object
		k1 := xxhash.Sum64(id.Key)

		s, ok := sfmap[k1]
		if !ok {
			return nil, nil
		}
		p := sel[s.ParentID]

		// then use the Table nme in the Select and it's parent
		// to find the resolver to use for this relationship
		k2 := mkkey(h, s.Table, p.Table)

		r, ok := rmap[k2]
		if !ok {
			return nil, nil
		}

		id := jsn.Value(id.Value)
		if len(id) == 0 {
			return nil, nil
		}

		go func(n int, id []byte, s *qcode.Select) {
			defer wg.Done()

			st := time.Now()

			b, err := r.Fn(req, id)
			if err != nil {
				cerr = fmt.Errorf("%s: %s", s.Table, err)
				return
			}

			if conf.EnableTracing {
				c.addTrace(sel, s.ID, st)
			}

			if len(r.Path) != 0 {
				b = jsn.Strip(b, r.Path)
			}

			var ob bytes.Buffer

			if len(s.Cols) != 0 {
				err = jsn.Filter(&ob, b, colsToList(s.Cols))
				if err != nil {
					cerr = fmt.Errorf("%s: %s", s.Table, err)
					return
				}

			} else {
				ob.WriteString("null")
			}

			to[n] = jsn.Field{[]byte(s.FieldName), ob.Bytes()}
		}(i, id, s)
	}
	wg.Wait()

	return to, cerr
}

func (c *coreContext) resolveSQL(qc *qcode.QCode, vars variables) (
	[]byte, uint32, error) {

	// var entry []byte
	// var key string

	// cacheEnabled := (conf.EnableTracing == false)

	// if cacheEnabled {
	// 	k := sha1.Sum([]byte(req.Query))
	// 	key = string(k[:])
	// 	entry, err = cache.Get(key)

	// 	if err != nil && err != bigcache.ErrEntryNotFound {
	// 		return emtpy, err
	// 	}

	// 	if len(entry) != 0 && err == nil {
	// 		return entry, nil
	// 	}
	// }

	stmt := &bytes.Buffer{}

	skipped, err := pcompile.Compile(qc, stmt)
	if err != nil {
		return nil, 0, err
	}

	t := fasttemplate.New(stmt.String(), openVar, closeVar)

	stmt.Reset()
	_, err = t.Execute(stmt, vars)

	if err == errNoUserID &&
		authFailBlock == authFailBlockPerQuery &&
		authCheck(c) == false {
		return nil, 0, errUnauthorized
	}

	if err != nil {
		return nil, 0, err
	}

	finalSQL := stmt.String()

	if conf.DebugLevel > 0 {
		os.Stdout.WriteString(finalSQL)
	}

	// if cacheEnabled {
	// 	if err = cache.Set(key, finalSQL); err != nil {
	// 		return err
	// 	}
	// }

	var st time.Time

	if conf.EnableTracing {
		st = time.Now()
	}

	var root json.RawMessage
	_, err = db.Query(pg.Scan(&root), finalSQL)

	if err != nil {
		return nil, 0, err
	}

	if conf.EnableTracing && len(qc.Query.Selects) != 0 {
		c.addTrace(
			qc.Query.Selects,
			qc.Query.Selects[0].ID,
			st)
	}

	return []byte(root), skipped, nil
}

func (c *coreContext) render(w io.Writer, data []byte) error {
	c.res.Data = json.RawMessage(data)
	return json.NewEncoder(w).Encode(c.res)
}

func (c *coreContext) addTrace(sel []qcode.Select, id int32, st time.Time) {
	et := time.Now()
	du := et.Sub(st)

	if c.res.Extensions == nil {
		c.res.Extensions = &extensions{&trace{
			Version:   1,
			StartTime: st,
			Execution: execution{},
		}}
	}

	c.res.Extensions.Tracing.EndTime = et
	c.res.Extensions.Tracing.Duration = du

	n := 1
	for i := id; i != 0; i = sel[i].ParentID {
		n++
	}
	path := make([]string, n)

	n--
	for i := id; ; i = sel[i].ParentID {
		path[n] = sel[i].Table
		if sel[i].ID == 0 {
			break
		}
		n--
	}

	tr := resolver{
		Path:        path,
		ParentType:  "Query",
		FieldName:   sel[id].Table,
		ReturnType:  "object",
		StartOffset: 1,
		Duration:    du,
	}

	c.res.Extensions.Tracing.Execution.Resolvers =
		append(c.res.Extensions.Tracing.Execution.Resolvers, tr)
}

func parentFieldIds(h *xxhash.Digest, sel []qcode.Select, skipped uint32) (
	[][]byte,
	map[uint64]*qcode.Select) {

	c := 0
	for i := range sel {
		s := &sel[i]
		if isSkipped(skipped, uint32(s.ID)) {
			c++
		}
	}

	// list of keys (and it's related value) to extract from
	// the db json response
	fm := make([][]byte, c)

	// mapping between the above extracted key and a Select
	// object
	sm := make(map[uint64]*qcode.Select, c)
	n := 0

	for i := range sel {
		s := &sel[i]

		if isSkipped(skipped, uint32(s.ID)) == false {
			continue
		}

		p := sel[s.ParentID]
		k := mkkey(h, s.Table, p.Table)

		if r, ok := rmap[k]; ok {
			fm[n] = r.IDField
			n++

			k := xxhash.Sum64(r.IDField)
			sm[k] = s
		}
	}

	return fm, sm
}

func isSkipped(n uint32, pos uint32) bool {
	return ((n & (1 << pos)) != 0)
}

func authCheck(ctx *coreContext) bool {
	return (ctx.Value(userIDKey) != nil)
}

func colsToList(cols []qcode.Column) []string {
	var f []string

	for i := range cols {
		f = append(f, cols[i].Name)
	}
	return f
}
