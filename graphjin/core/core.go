package core

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/dosco/super-graph/core/internal/psql"
	"github.com/dosco/super-graph/core/internal/qcode"
	"github.com/dosco/super-graph/core/internal/sdata"
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

func (sg *SuperGraph) initSchema() error {
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
		sg.dbinfo, err = sdata.GetDBInfo(sg.db, schema, sg.conf.Blocklist)
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

	sg.schema, err = sdata.NewDBSchema(sg.dbinfo, getDBTableAliases(sg.conf))
	return err
}

func (sg *SuperGraph) initCompilers() error {
	var err error

	sg.qc, err = qcode.NewCompiler(sg.schema, qcode.Config{
		DefaultBlock: sg.conf.DefaultBlock,
	})
	if err != nil {
		return err
	}

	if err := addRoles(sg.conf, sg.qc); err != nil {
		return err
	}

	sg.pc = psql.NewCompiler(psql.Config{Vars: sg.conf.Vars})
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
	var res qres

	urq := c.sg.abacEnabled && c.op == qcode.QTMutation // userRoleQuery
	rq := rquery{op: c.op, name: c.name, query: []byte(query), vars: vars}
	cq := &cquery{q: rq}
	res.q = cq
	res.role = role

	conn, err := c.sg.db.Conn(c)
	if err != nil {
		return res, err
	}
	defer conn.Close()

	if c.sg.conf.SetUserID {
		if err := c.setLocalUserID(conn); err != nil {
			return res, err
		}
	}

	if v := c.Value(UserRoleKey); v != nil {
		res.role = v.(string)
	} else if urq {
		res.role, err = c.executeRoleQuery(conn, res.role)
	}

	if err != nil {
		return res, err
	}

	if err = c.sg.compileQuery(cq, res.role); err != nil {
		return res, err
	}

	args, err := c.sg.argList(c, cq.st.md, vars)
	if err != nil {
		return res, err
	}

	// var stime time.Time

	// if c.sg.conf.EnableTracing {
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

	cur, err := c.sg.encryptCursor(cq.st.qc, res.data)
	if err != nil {
		return res, err
	}

	res.data = cur.data
	//res.role = role

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

func (c *scontext) executeRoleQuery(conn *sql.Conn, role string) (string, error) {
	uid := c.Value(UserIDKey)
	if uid == nil {
		return "anon", nil
	}

	err := conn.QueryRowContext(c, c.sg.roleStmt, uid, role).Scan(&role)
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
			c.sg.log.Printf("INF table '%s' skipped as it requires $user_id", sel.Table)
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
