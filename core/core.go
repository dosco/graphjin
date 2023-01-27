package core

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dosco/graphjin/core/v3/internal/allow"
	"github.com/dosco/graphjin/core/v3/internal/jsn"
	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/internal/valid"
)

var (
	decPrefix   = []byte(`__gj/enc:`)
	ErrNotFound = errors.New("not found in prepared statements")
)

type OpType int

const (
	OpUnknown OpType = iota
	OpQuery
	OpSubscription
	OpMutation
)

// type extensions struct {
// 	Tracing *trace `json:"tracing,omitempty"`
// }

// type trace struct {
// 	Version   int           `json:"version"`
// 	StartTime time.Time     `json:"startTime"`
// 	EndTime   time.Time     `json:"endTime"`
// 	Duration  time.Duration `json:"duration"`
// 	Execution execution     `json:"execution"`
// }

// type execution struct {
// 	Resolvers []resolver `json:"resolvers"`
// }

// type resolver struct {
// 	Path        []string      `json:"path"`
// 	ParentType  string        `json:"parentType"`
// 	FieldName   string        `json:"fieldName"`
// 	ReturnType  string        `json:"returnType"`
// 	StartOffset int           `json:"startOffset"`
// 	Duration    time.Duration `json:"duration"`
// }

func (gj *graphjin) getIntroResult() (data json.RawMessage, err error) {
	var ok bool
	if data, ok = gj.cache.Get("_intro"); ok {
		return
	}
	if data, err = gj.introQuery(); err != nil {
		return
	}
	gj.cache.Set("_intro", data)
	return
}

func (gj *graphjin) initDiscover() (err error) {
	switch gj.conf.DBType {
	case "":
		gj.dbtype = "postgres"
	case "mssql":
		gj.dbtype = "mysql"
	default:
		gj.dbtype = gj.conf.DBType
	}

	if err = gj._initDiscover(); err != nil {
		err = fmt.Errorf("%s: %w", gj.dbtype, err)
	}
	return
}

func (gj *graphjin) _initDiscover() (err error) {
	if gj.prod && gj.conf.EnableSchema {
		b, err := gj.fs.Get("db.graphql")
		if err != nil {
			return err
		}
		ds, err := qcode.ParseSchema(b)
		if err != nil {
			return err
		}
		gj.dbinfo = sdata.NewDBInfo(ds.Type,
			ds.Version,
			ds.Schema,
			"",
			ds.Columns,
			ds.Functions,
			gj.conf.Blocklist)
	}

	// gj.dbinfo could be preset due to tests or db
	// watcher reloading
	if gj.dbinfo == nil {
		gj.dbinfo, err = sdata.GetDBInfo(
			gj.db,
			gj.dbtype,
			gj.conf.Blocklist)
		if err != nil {
			return
		}
	}

	if !gj.prod && gj.conf.EnableSchema {
		var buf bytes.Buffer
		if err := writeSchema(gj.dbinfo, &buf); err != nil {
			return err
		}
		err = gj.fs.Put("db.graphql", buf.Bytes())
		if err != nil {
			return
		}
	}

	return
}

func (gj *graphjin) initSchema() error {
	if err := gj._initSchema(); err != nil {
		return fmt.Errorf("%s: %w", gj.dbtype, err)
	}
	return nil
}

func (gj *graphjin) _initSchema() (err error) {
	if len(gj.dbinfo.Tables) == 0 {
		return fmt.Errorf("no tables found in database")
	}

	schema := gj.dbinfo.Schema
	for i, t := range gj.conf.Tables {
		if t.Schema == "" {
			gj.conf.Tables[i].Schema = schema
			t.Schema = schema
		}
		// skip aliases
		if t.Table != "" && t.Type == "" {
			continue
		}
		if err = gj.addTableInfo(t); err != nil {
			return
		}
	}

	if err = addTables(gj.conf, gj.dbinfo); err != nil {
		return
	}

	if err = addForeignKeys(gj.conf, gj.dbinfo); err != nil {
		return
	}

	gj.schema, err = sdata.NewDBSchema(
		gj.dbinfo,
		getDBTableAliases(gj.conf))

	if err != nil {
		return
	}

	if !gj.prod && gj.conf.EnableIntrospection {
		var introJSON json.RawMessage
		introJSON, err = gj.getIntroResult()
		if err != nil {
			return
		}
		err = gj.fs.Put("intro.json", []byte(introJSON))
		if err != nil {
			return
		}
	}
	return
}

func (gj *graphjin) initCompilers() (err error) {
	qcc := qcode.Config{
		TConfig:         gj.tmap,
		DefaultBlock:    gj.conf.DefaultBlock,
		DefaultLimit:    gj.conf.DefaultLimit,
		DisableAgg:      gj.conf.DisableAgg,
		DisableFuncs:    gj.conf.DisableFuncs,
		EnableCamelcase: gj.conf.EnableCamelcase,
		DBSchema:        gj.schema.DBSchema(),
		Validators:      valid.Validators,
	}

	gj.qc, err = qcode.NewCompiler(gj.schema, qcc)
	if err != nil {
		return
	}

	if err = addRoles(gj.conf, gj.qc); err != nil {
		return
	}

	gj.pc = psql.NewCompiler(psql.Config{
		Vars:            gj.conf.Vars,
		DBType:          gj.schema.DBType(),
		DBVersion:       gj.schema.DBVersion(),
		SecPrefix:       gj.pf,
		EnableCamelcase: gj.conf.EnableCamelcase,
	})
	return
}

func (gj *graphjin) executeRoleQuery(c context.Context,
	conn *sql.Conn,
	vmap map[string]json.RawMessage,
	rc *ReqConfig,
) (role string, err error) {
	if c.Value(UserIDKey) == nil {
		role = "anon"
		return
	}

	ar, err := gj.argList(c,
		gj.roleStmtMD,
		vmap,
		rc,
		false)
	if err != nil {
		return
	}

	needsConn := ((rc != nil && rc.Tx == nil) && conn == nil)
	if needsConn {
		c1, span := gj.spanStart(c, "Get Connection")
		defer span.End()

		err = retryOperation(c1, func() (err1 error) {
			conn, err1 = gj.db.Conn(c1)
			return
		})
		if err != nil {
			span.Error(err)
			return
		}
		defer conn.Close()
	}

	c1, span := gj.spanStart(c, "Execute Role Query")
	defer span.End()

	err = retryOperation(c1, func() error {
		var row *sql.Row
		if rc != nil && rc.Tx != nil {
			row = rc.Tx.QueryRowContext(c1, gj.roleStmt, ar.values...)
		} else {
			row = conn.QueryRowContext(c1, gj.roleStmt, ar.values...)
		}
		return row.Scan(&role)
	})
	if err != nil {
		span.Error(err)
		return
	}

	span.SetAttributesString(StringAttr{"role", role})
	return
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

func (r *Result) Namespace() string {
	return r.ns
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

func (r *Result) CacheControl() string {
	return r.cacheControl
}

// func (c *gstate) addTrace(sel []qcode.Select, id int32, st time.Time) {
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

func (s *gstate) debugLogStmt() {
	st := s.cs.st

	if st.qc == nil {
		return
	}

	for _, sel := range st.qc.Selects {
		if sel.SkipRender == qcode.SkipTypeUserNeeded {
			s.gj.log.Printf("Field skipped, requires $user_id or table not added to anon role: %s", sel.FieldName)
		}
		if sel.SkipRender == qcode.SkipTypeBlocked {
			s.gj.log.Printf("Field skipped, blocked: %s", sel.FieldName)
		}
	}
}

func (gj *graphjin) saveToAllowList(qc *qcode.QCode, ns string) (err error) {
	if gj.conf.DisableAllowList {
		return nil
	}

	item := allow.Item{
		Namespace: ns,
		Name:      qc.Name,
		Query:     qc.Query,
		Fragments: make([]allow.Fragment, len(qc.Fragments)),
	}

	if len(qc.ActionVal) != 0 {
		var buf bytes.Buffer
		if err = jsn.Clear(&buf, qc.ActionVal); err != nil {
			return
		}
		item.ActionJSON = map[string]json.RawMessage{
			qc.ActionVar: json.RawMessage(buf.Bytes()),
		}
	}

	for i, f := range qc.Fragments {
		item.Fragments[i] = allow.Fragment{Name: f.Name, Value: f.Value}
	}

	return gj.allowList.Set(item)
}

func (gj *graphjin) spanStart(c context.Context, name string) (context.Context, Spaner) {
	return gj.trace.Start(c, name)
}

func retryOperation(c context.Context, fn func() error) (err error) {
	jitter := []int{50, 100, 200}
	for i := 0; i < 3; i++ {
		if err = fn(); err == nil {
			return
		}
		d := time.Duration(jitter[i])
		time.Sleep(d * time.Millisecond)
	}
	return
}
