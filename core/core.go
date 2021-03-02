package core

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/dosco/graphjin/core/internal/psql"
	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/dosco/graphjin/core/internal/sdata"
)

type OpType int

const (
	OpUnknown OpType = iota
	OpQuery
	OpSubscription
	OpMutation
)

type extensions struct {
	Tracing *trace `json:"tracing,omitempty"`
}

type trace struct {
	Version   int           `json:"version"`
	StartTime time.Time     `json:"startTime"`
	EndTime   time.Time     `json:"endTime"`
	Duration  time.Duration `json:"duration"`
	Execution execution     `json:"execution"`
}

type execution struct {
	Resolvers []resolver `json:"resolvers"`
}

type resolver struct {
	Path        []string      `json:"path"`
	ParentType  string        `json:"parentType"`
	FieldName   string        `json:"fieldName"`
	ReturnType  string        `json:"returnType"`
	StartOffset int           `json:"startOffset"`
	Duration    time.Duration `json:"duration"`
}

type scontext struct {
	context.Context

	gj   *GraphJin
	op   qcode.QType
	rc   *ReqConfig
	name string
}

type qres struct {
	q    *cquery
	data []byte
	role string
}

func (gj *GraphJin) initDiscover() error {
	if err := gj._initDiscover(); err != nil {
		return fmt.Errorf("%s: %w", gj.conf.DBType, err)
	}
	return nil
}

func (gj *GraphJin) _initDiscover() error {
	var err error

	if gj.conf.DBType == "" {
		gj.conf.DBType = "postgres"
	}

	// If gj.dbinfo is not null then it's probably set
	// for tests
	if gj.dbinfo == nil {
		gj.dbinfo, err = sdata.GetDBInfo(
			gj.db,
			gj.conf.DBType,
			gj.conf.Blocklist)
	}

	return err
}

func (gj *GraphJin) initSchema() error {
	if err := gj._initSchema(); err != nil {
		return fmt.Errorf("%s: %w", gj.conf.DBType, err)
	}
	return nil
}

func (gj *GraphJin) _initSchema() error {
	var err error

	if len(gj.dbinfo.Tables) == 0 {
		return fmt.Errorf("no tables found in database")
	}

	if err := addTables(gj.conf, gj.dbinfo); err != nil {
		return err
	}

	if err := addForeignKeys(gj.conf, gj.dbinfo); err != nil {
		return err
	}

	gj.schema, err = sdata.NewDBSchema(
		gj.dbinfo,
		getDBTableAliases(gj.conf))

	return err
}

func (gj *GraphJin) initCompilers() error {
	var err error

	qcc := qcode.Config{
		DefaultBlock:     gj.conf.DefaultBlock,
		DefaultLimit:     gj.conf.DefaultLimit,
		EnableInflection: gj.conf.EnableInflection,
		DBSchema:         gj.schema.DBSchema(),
	}

	if gj.allowList != nil && gj.prod {
		qcc.FragmentFetcher = gj.allowList.FragmentFetcher()
	}

	gj.qc, err = qcode.NewCompiler(gj.schema, qcc)
	if err != nil {
		return err
	}

	if err := addRoles(gj.conf, gj.qc); err != nil {
		return err
	}

	gj.pc = psql.NewCompiler(psql.Config{
		Vars:      gj.conf.Vars,
		DBType:    gj.schema.DBType(),
		DBVersion: gj.schema.DBVersion(),
	})
	return nil
}

func (c *scontext) execQuery(query string, vars []byte, role string) (qres, error) {
	res, err := c.resolveSQL(query, vars, role)
	if err != nil {
		return res, err
	}

	if c.gj.conf.Debug {
		c.debugLog(&res.q.st)
	}

	if len(res.data) == 0 || res.q.st.qc.Remotes == 0 {
		return res, nil
	}

	return c.execRemoteJoin(res)
}

func (c *scontext) resolveSQL(query string, vars []byte, role string) (qres, error) {
	var res qres

	rq := rquery{op: c.op, name: c.name, query: []byte(query), vars: vars}
	cq := &cquery{q: rq}
	res.q = cq
	res.role = role

	conn, err := c.gj.db.Conn(c)
	if err != nil {
		return res, err
	}
	defer conn.Close()

	if c.gj.conf.SetUserID {
		if err := c.setLocalUserID(conn); err != nil {
			return res, err
		}
	}

	if v := c.Value(UserRoleKey); v != nil {
		res.role = v.(string)

	} else if c.gj.abacEnabled && c.op == qcode.QTMutation {
		res.role, err = c.executeRoleQuery(conn)
	}

	if err != nil {
		return res, err
	}

	if err = c.gj.compileQuery(cq, res.role); err != nil {
		return res, err
	}

	args, err := c.gj.argList(c, cq.st.md, vars, c.rc)
	if err != nil {
		return res, err
	}

	// var stime time.Time

	// if c.gj.conf.EnableTracing {
	// 	stime = time.Now()
	// }

	row := conn.QueryRowContext(c, cq.st.sql, args.values...)
	if cq.roleArg {
		err = row.Scan(&res.role, &res.data)
	} else {
		err = row.Scan(&res.data)
	}

	if err == sql.ErrNoRows {
		return res, err
	} else if err != nil {
		return res, err
	}

	cur, err := c.gj.encryptCursor(cq.st.qc, res.data)
	if err != nil {
		return res, err
	}

	res.data = cur.data

	if c.gj.allowList != nil && !c.gj.prod {
		if err := c.gj.allowList.Set(vars, query); err != nil {
			return res, err
		}
	}

	// if len(stmts) > 1 {
	// 	if st = findStmt(role, stmts); st == nil {
	// 		return nil, nil, fmt.Errorf("invalid role '%s' returned", role)
	// 	}
	// }

	// if c.gj.conf.EnableTracing {
	// 	for _, id := range st.qc.Roots {
	// 		c.addTrace(st.qc.Selects, id, stime)
	// 	}
	// }

	return res, nil
}

func (c *scontext) executeRoleQuery(conn *sql.Conn) (string, error) {
	var role string
	var ar args
	var err error

	if c.Value(UserIDKey) == nil {
		return "anon", nil
	}

	if ar, err = c.gj.roleQueryArgList(c); err != nil {
		return "", err
	}

	err = conn.QueryRowContext(c, c.gj.roleStmt, ar.values...).Scan(&role)
	return role, err
}

func (c *scontext) setLocalUserID(conn *sql.Conn) error {
	var err error

	if v := c.Value(UserIDKey); v == nil {
		return nil
	} else {
		switch v1 := v.(type) {
		case string:
			_, err = conn.ExecContext(c, `SET SESSION "user.id" = '`+v1+`'`)

		case int:
			_, err = conn.ExecContext(c, `SET SESSION "user.id" = `+strconv.Itoa(v1))
		}
	}

	return err
}

func (r *Result) Operation() OpType {
	switch r.op {
	case qcode.QTQuery:
		return OpQuery

	case qcode.QTMutation, qcode.QTInsert, qcode.QTUpdate, qcode.QTUpsert, qcode.QTDelete:
		return OpMutation

	default:
		return -1
	}
}

func (r *Result) OperationName() string {
	return r.op.String()
}

func (r *Result) QueryName() string {
	return r.name
}

func (r *Result) Role() string {
	return r.role
}

func (r *Result) SQL() string {
	return r.sql
}

// func (c *scontext) addTrace(sel []qcode.Select, id int32, st time.Time) {
// 	et := time.Now()
// 	du := et.Sub(st)

// 	if c.res.Extensions == nil {
// 		c.res.Extensions = &extensions{&trace{
// 			Version:   1,
// 			StartTime: st,
// 			Execution: execution{},
// 		}}
// 	}

// 	c.res.Extensions.Tracing.EndTime = et
// 	c.res.Extensions.Tracing.Duration = du

// 	n := 1
// 	for i := id; i != -1; i = sel[i].ParentID {
// 		n++
// 	}
// 	path := make([]string, n)

// 	n--
// 	for i := id; ; i = sel[i].ParentID {
// 		path[n] = sel[i].Name
// 		if sel[i].ParentID == -1 {
// 			break
// 		}
// 		n--
// 	}

// 	tr := resolver{
// 		Path:        path,
// 		ParentType:  "Query",
// 		FieldName:   sel[id].Name,
// 		ReturnType:  "object",
// 		StartOffset: 1,
// 		Duration:    du,
// 	}

// 	c.res.Extensions.Tracing.Execution.Resolvers =
// 		append(c.res.Extensions.Tracing.Execution.Resolvers, tr)
// }

func (c *scontext) debugLog(st *stmt) {
	for _, sel := range st.qc.Selects {
		if sel.SkipRender == qcode.SkipTypeUserNeeded {
			c.gj.log.Printf("Field skipped: %s, Requires $user_id", sel.FieldName)
		}
	}
}

// func findStmt(role string, stmts []stmt) *stmt {
// 	for i := range stmts {
// 		if stmts[i].role.Name != role {
// 			continue
// 		}
// 		return &stmts[i]
// 	}
// 	return nil
// }
