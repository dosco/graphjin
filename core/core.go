package core

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dosco/super-graph/core/internal/psql"
	"github.com/dosco/super-graph/core/internal/qcode"

	"github.com/valyala/fasttemplate"
)

const (
	OpQuery int = iota
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

	sg    *SuperGraph
	query string
	vars  json.RawMessage
	role  string
	res   Result
}

func (sg *SuperGraph) initCompilers() error {
	var err error

	// If sg.di is not null then it's probably set
	// for tests
	if sg.dbinfo == nil {
		sg.dbinfo, err = psql.GetDBInfo(sg.db)
		if err != nil {
			return err
		}
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
		Blocklist:    sg.conf.Blocklist,
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

func (c *scontext) execQuery() ([]byte, error) {
	var data []byte
	var st *stmt
	var err error

	if c.sg.conf.UseAllowList {
		data, st, err = c.resolvePreparedSQL()
	} else {
		data, st, err = c.resolveSQL()
	}

	if err != nil {
		return nil, err
	}

	if len(data) == 0 || st.skipped == 0 {
		return data, nil
	}

	// return c.sg.execRemoteJoin(st, data, c.req.hdr)
	return c.sg.execRemoteJoin(st, data, nil)
}

func (c *scontext) resolvePreparedSQL() ([]byte, *stmt, error) {
	var tx *sql.Tx
	var err error

	mutation := (c.res.op == qcode.QTMutation)
	useRoleQuery := c.sg.abacEnabled && mutation
	useTx := useRoleQuery || c.sg.conf.SetUserID

	if useTx {
		if tx, err = c.sg.db.BeginTx(c, nil); err != nil {
			return nil, nil, err
		}
		defer tx.Rollback() //nolint: errcheck
	}

	if c.sg.conf.SetUserID {
		if err := setLocalUserID(c, tx); err != nil {
			return nil, nil, err
		}
	}

	var role string

	if useRoleQuery {
		if role, err = c.executeRoleQuery(tx); err != nil {
			return nil, nil, err
		}

	} else if v := c.Value(UserRoleKey); v != nil {
		role = v.(string)

	} else {
		role = c.role

	}

	c.res.role = role

	ps, ok := c.sg.prepared[stmtHash(c.res.name, role)]
	if !ok {
		return nil, nil, errNotFound
	}
	c.res.sql = ps.st.sql

	var root []byte
	var row *sql.Row

	varsList, err := c.argList(ps.args)
	if err != nil {
		return nil, nil, err
	}

	if useTx {
		row = tx.Stmt(ps.sd).QueryRow(varsList...)
	} else {
		row = ps.sd.QueryRow(varsList...)
	}

	if ps.roleArg {
		err = row.Scan(&role, &root)
	} else {
		err = row.Scan(&root)
	}

	if err != nil {
		return nil, nil, err
	}

	c.role = role

	if useTx {
		if err := tx.Commit(); err != nil {
			return nil, nil, err
		}
	}

	if root, err = c.sg.encryptCursor(ps.st.qc, root); err != nil {
		return nil, nil, err
	}

	return root, &ps.st, nil
}

func (c *scontext) resolveSQL() ([]byte, *stmt, error) {
	var tx *sql.Tx
	var err error

	mutation := (c.res.op == qcode.QTMutation)
	useRoleQuery := c.sg.abacEnabled && mutation
	useTx := useRoleQuery || c.sg.conf.SetUserID

	if useTx {
		if tx, err = c.sg.db.BeginTx(c, nil); err != nil {
			return nil, nil, err
		}
		defer tx.Rollback() //nolint: errcheck
	}

	if c.sg.conf.SetUserID {
		if err := setLocalUserID(c, tx); err != nil {
			return nil, nil, err
		}
	}

	if useRoleQuery {
		if c.role, err = c.executeRoleQuery(tx); err != nil {
			return nil, nil, err
		}

	} else if v := c.Value(UserRoleKey); v != nil {
		c.role = v.(string)
	}

	stmts, err := c.sg.buildStmt(c.res.op, []byte(c.query), c.vars, c.role)
	if err != nil {
		return nil, nil, err
	}
	st := &stmts[0]

	t := fasttemplate.New(st.sql, openVar, closeVar)
	buf := &bytes.Buffer{}

	_, err = t.ExecuteFunc(buf, c.argMap())
	if err != nil {
		return nil, nil, err
	}
	finalSQL := buf.String()

	// var stime time.Time

	// if c.sg.conf.EnableTracing {
	// 	stime = time.Now()
	// }

	var root []byte
	var role string
	var row *sql.Row

	// defaultRole := c.role

	if useTx {
		row = tx.QueryRow(finalSQL)
	} else {
		row = c.sg.db.QueryRow(finalSQL)
	}

	if len(stmts) > 1 {
		err = row.Scan(&role, &root)
	} else {
		err = row.Scan(&root)
	}

	c.res.sql = finalSQL

	if len(role) == 0 {
		c.res.role = c.role
	} else {
		c.res.role = role
	}

	if err != nil {
		return nil, nil, err
	}

	if useTx {
		if err := tx.Commit(); err != nil {
			return nil, nil, err
		}
	}

	if root, err = c.sg.encryptCursor(st.qc, root); err != nil {
		return nil, nil, err
	}

	if c.sg.allowList.IsPersist() {
		if err := c.sg.allowList.Set(c.vars, c.query, ""); err != nil {
			return nil, nil, err
		}
	}

	if len(stmts) > 1 {
		if st = findStmt(role, stmts); st == nil {
			return nil, nil, fmt.Errorf("invalid role '%s' returned", role)
		}
	}

	// if c.sg.conf.EnableTracing {
	// 	for _, id := range st.qc.Roots {
	// 		c.addTrace(st.qc.Selects, id, stime)
	// 	}
	// }

	return root, st, nil
}

func (c *scontext) executeRoleQuery(tx *sql.Tx) (string, error) {
	userID := c.Value(UserIDKey)

	if userID == nil {
		return "anon", nil
	}

	var role string
	row := c.sg.getRole.QueryRow(userID, c.role)

	if err := row.Scan(&role); err != nil {
		return "", err
	}

	return role, nil
}

func (r *Result) Operation() int {
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

func findStmt(role string, stmts []stmt) *stmt {
	for i := range stmts {
		if stmts[i].role.Name != role {
			continue
		}
		return &stmts[i]
	}
	return nil
}
