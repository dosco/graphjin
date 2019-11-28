package serv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/dosco/super-graph/qcode"
	"github.com/jackc/pgx/v4"
	"github.com/valyala/fasttemplate"
)

type coreContext struct {
	req gqlReq
	res gqlResp
	context.Context
}

func (c *coreContext) handleReq(w io.Writer, req *http.Request) error {
	c.req.ref = req.Referer()
	c.req.hdr = req.Header

	if len(c.req.Vars) == 2 {
		c.req.Vars = nil
	}

	if authCheck(c) {
		c.req.role = "user"
	} else {
		c.req.role = "anon"
	}

	b, err := c.execQuery()
	if err != nil {
		return err
	}

	return c.render(w, b)
}

func (c *coreContext) execQuery() ([]byte, error) {
	var data []byte
	var st *stmt
	var err error

	if conf.Production {
		data, st, err = c.resolvePreparedSQL()
		if err != nil {
			logger.Error().
				Err(err).
				Str("default_role", c.req.role).
				Msg(c.req.Query)

			return nil, errors.New("query failed. check logs for error")
		}

	} else {
		if data, st, err = c.resolveSQL(); err != nil {
			return nil, err
		}
	}

	return execRemoteJoin(st, data, c.req.hdr)
}

func (c *coreContext) resolvePreparedSQL() ([]byte, *stmt, error) {
	var tx pgx.Tx
	var err error

	qt := qcode.GetQType(c.req.Query)
	mutation := (qt == qcode.QTMutation)
	anonQuery := (qt == qcode.QTQuery && c.req.role == "anon")

	useRoleQuery := len(conf.RolesQuery) != 0 && mutation
	useTx := useRoleQuery || conf.DB.SetUserID

	if useTx {
		if tx, err = db.Begin(c); err != nil {
			return nil, nil, err
		}
		defer tx.Rollback(c) //nolint: errcheck
	}

	if conf.DB.SetUserID {
		if err := setLocalUserID(c, tx); err != nil {
			return nil, nil, err
		}
	}

	var role string

	if useRoleQuery {
		if role, err = c.executeRoleQuery(tx); err != nil {
			return nil, nil, err
		}

	} else if v := c.Value(userRoleKey); v != nil {
		role = v.(string)

	} else {
		role = c.req.role

	}

	ps, ok := _preparedList[gqlHash(c.req.Query, c.req.Vars, role)]
	if !ok {
		return nil, nil, errUnauthorized
	}

	var root []byte
	var row pgx.Row

	vars, err := argList(c, ps.args)
	if err != nil {
		return nil, nil, err
	}

	if useTx {
		row = tx.QueryRow(c, ps.sd.SQL, vars...)
	} else {
		row = db.QueryRow(c, ps.sd.SQL, vars...)
	}

	if mutation || anonQuery {
		err = row.Scan(&root)
	} else {
		err = row.Scan(&role, &root)
	}

	if len(role) == 0 {
		logger.Debug().Str("default_role", c.req.role).Msg(c.req.Query)
	} else {
		logger.Debug().Str("default_role", c.req.role).Str("role", role).Msg(c.req.Query)
	}

	if err != nil {
		return nil, nil, err
	}

	c.req.role = role

	if useTx {
		if err := tx.Commit(c); err != nil {
			return nil, nil, err
		}
	}

	return root, ps.st, nil
}

func (c *coreContext) resolveSQL() ([]byte, *stmt, error) {
	var tx pgx.Tx
	var err error

	qt := qcode.GetQType(c.req.Query)
	mutation := (qt == qcode.QTMutation)
	//anonQuery := (qt == qcode.QTQuery && c.req.role == "anon")

	useRoleQuery := len(conf.RolesQuery) != 0 && mutation
	useTx := useRoleQuery || conf.DB.SetUserID

	if useTx {
		if tx, err = db.Begin(c); err != nil {
			return nil, nil, err
		}
		defer tx.Rollback(c) //nolint: errcheck
	}

	if conf.DB.SetUserID {
		if err := setLocalUserID(c, tx); err != nil {
			return nil, nil, err
		}
	}

	if useRoleQuery {
		if c.req.role, err = c.executeRoleQuery(tx); err != nil {
			return nil, nil, err
		}

	} else if v := c.Value(userRoleKey); v != nil {
		c.req.role = v.(string)
	}

	stmts, err := buildStmt(qt, []byte(c.req.Query), c.req.Vars, c.req.role)
	if err != nil {
		return nil, nil, err
	}
	st := &stmts[0]

	t := fasttemplate.New(st.sql, openVar, closeVar)
	buf := &bytes.Buffer{}

	_, err = t.ExecuteFunc(buf, argMap(c, c.req.Vars))
	if err != nil {
		return nil, nil, err
	}
	finalSQL := buf.String()

	var stime time.Time

	if conf.EnableTracing {
		stime = time.Now()
	}

	var root []byte
	var role string
	var row pgx.Row

	defaultRole := c.req.role

	if useTx {
		row = tx.QueryRow(c, finalSQL)
	} else {
		row = db.QueryRow(c, finalSQL)
	}

	if len(stmts) == 1 {
		err = row.Scan(&root)
	} else {
		err = row.Scan(&role, &root)
	}

	if len(role) == 0 {
		logger.Debug().Str("default_role", defaultRole).Msg(c.req.Query)
	} else {
		logger.Debug().Str("default_role", defaultRole).Str("role", role).Msg(c.req.Query)
	}

	if err != nil {
		return nil, nil, err
	}

	if useTx {
		if err := tx.Commit(c); err != nil {
			return nil, nil, err
		}
	}

	if !conf.Production {
		_allowList.add(&c.req)
	}

	if len(stmts) > 1 {
		if st = findStmt(role, stmts); st == nil {
			return nil, nil, fmt.Errorf("invalid role '%s' returned", role)
		}
	}

	if conf.EnableTracing {
		for _, id := range st.qc.Roots {
			c.addTrace(st.qc.Selects, id, stime)
		}
	}

	return root, st, nil
}

func (c *coreContext) executeRoleQuery(tx pgx.Tx) (string, error) {
	var role string
	row := tx.QueryRow(c, "_sg_get_role", c.req.role, 1)

	if err := row.Scan(&role); err != nil {
		return "", err
	}

	return role, nil
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
	for i := id; i != -1; i = sel[i].ParentID {
		n++
	}
	path := make([]string, n)

	n--
	for i := id; ; i = sel[i].ParentID {
		path[n] = sel[i].Table
		if sel[i].ParentID == -1 {
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

		if !isSkipped(skipped, uint32(s.ID)) {
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

func setLocalUserID(c context.Context, tx pgx.Tx) error {
	var err error
	if v := c.Value(userIDKey); v != nil {
		_, err = tx.Exec(c, fmt.Sprintf(`SET LOCAL "user.id" = %s;`, v))
	}

	return err
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
