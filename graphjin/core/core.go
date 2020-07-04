package core

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/dosco/super-graph/core/internal/psql"
	"github.com/dosco/super-graph/core/internal/qcode"
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

	sg   *SuperGraph
	op   qcode.QType
	name string
}

type qres struct {
	q    *cquery
	data []byte
	role string
}

func (sg *SuperGraph) initCompilers() error {
	var err error
	var schema string

	if sg.conf.DBSchema == "" {
		schema = "public"
	} else {
		schema = sg.conf.DBSchema
	}

	// If sg.di is not null then it's probably set
	// for tests
	if sg.dbinfo == nil {
		sg.dbinfo, err = psql.GetDBInfo(sg.db, schema, sg.conf.Blocklist)
		if err != nil {
			return err
		}

	}

	if len(sg.dbinfo.Tables) == 0 {
		return fmt.Errorf("no tables found in database (schema: %s)", schema)
	}

	if err = addTables(sg.conf, sg.dbinfo); err != nil {
		return err
	}

	if err = addForeignKeys(sg.conf, sg.dbinfo); err != nil {
		return err
	}

	sg.schema, err = psql.NewDBSchema(sg.dbinfo, getDBTableAliases(sg.conf))
	if err != nil {
		return err
	}

	sg.qc, err = qcode.NewCompiler(qcode.Config{
		DefaultBlock: sg.conf.DefaultBlock,
	})
	if err != nil {
		return err
	}

	if err := addRoles(sg.conf, sg.qc); err != nil {
		return err
	}

	sg.pc = psql.NewCompiler(psql.Config{
		Schema: sg.schema,
		Vars:   sg.conf.Vars,
	})

	return nil
}

func (c *scontext) execQuery(query string, vars []byte, role string) (qres, error) {
	res, err := c.resolveSQL(query, vars, role)
	if err != nil {
		return res, err
	}

	if c.sg.conf.Debug {
		c.debugLog(&res.q.st)
	}

	if len(res.data) == 0 || !res.q.st.md.HasRemotes() {
		return res, nil
	}

	// return c.sg.execRemoteJoin(st, data, c.req.hdr)
	return c.sg.execRemoteJoin(res, nil)
}

func (c *scontext) resolveSQL(query string, vars []byte, role string) (qres, error) {
	var tx *sql.Tx
	var err error

	var res qres
	rq := rquery{op: c.op, name: c.name, query: []byte(query), vars: vars}
	cq := &cquery{q: rq}
	res.q = cq

	mutation := (c.op == qcode.QTMutation)
	urq := c.sg.abacEnabled && mutation // userRoleQuery
	useTx := urq || c.sg.conf.SetUserID

	if useTx {
		if tx, err = c.sg.db.BeginTx(c, nil); err != nil {
			return res, err
		}
		defer tx.Rollback() //nolint: errcheck
	}

	if c.sg.conf.SetUserID {
		if err := setLocalUserID(c, tx); err != nil {
			return res, err
		}
	}

	if v := c.Value(UserRoleKey); v != nil {
		role = v.(string)
	} else if urq {
		role, err = c.executeRoleQuery(tx, role)
	}

	if err != nil {
		return res, err
	}

	if err = c.sg.compileQuery(cq, role); err != nil {
		return res, err
	}

	varList, err := c.sg.argList(c, cq.st.md, vars)
	if err != nil {
		return res, err
	}

	// var stime time.Time

	// if c.sg.conf.EnableTracing {
	// 	stime = time.Now()
	// }

	var row *sql.Row

	if useTx {
		row = tx.QueryRowContext(c, cq.st.sql, varList...)
	} else {
		row = c.sg.db.QueryRowContext(c, cq.st.sql, varList...)
	}

	if cq.roleArg {
		err = row.Scan(&res.role, &res.data)
	} else {
		err = row.Scan(&res.data)
	}

	if err != nil {
		return res, err
	}

	res.role = role

	if useTx {
		if err := tx.Commit(); err != nil {
			return res, err
		}
	}

	res.data, err = c.sg.encryptCursor(cq.st.qc, res.data)
	if err != nil {
		return res, err
	}

	if c.sg.allowList.IsPersist() {
		if err := c.sg.allowList.Set(vars, query, ""); err != nil {
			return res, err
		}
	}

	// if len(stmts) > 1 {
	// 	if st = findStmt(role, stmts); st == nil {
	// 		return nil, nil, fmt.Errorf("invalid role '%s' returned", role)
	// 	}
	// }

	// if c.sg.conf.EnableTracing {
	// 	for _, id := range st.qc.Roots {
	// 		c.addTrace(st.qc.Selects, id, stime)
	// 	}
	// }

	return res, nil
}

func (c *scontext) executeRoleQuery(tx *sql.Tx, role string) (string, error) {
	if uid := c.Value(UserIDKey); uid == nil {
		return "anon", nil
	}

	var nr string
	err := c.sg.db.QueryRow(c.sg.roleStmt, role).Scan(&nr)

	return nr, err
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
		switch sel.SkipRender {
		case qcode.SkipTypeUserNeeded:
			c.sg.log.Printf("INF table '%s' skipped as it requires $user_id", sel.Name)

		case qcode.SkipTypeTableNotFound:
			c.sg.log.Printf("INF table '%s' skipped its not added to the 'anon' role config", sel.Name)
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
