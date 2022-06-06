package core

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	cuejson "cuelang.org/go/encoding/json"
	"github.com/avast/retry-go"
	"github.com/dosco/graphjin/core/internal/psql"
	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/dosco/graphjin/core/internal/sdata"
	"github.com/go-playground/validator/v10"
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

type gcontext struct {
	context.Context

	gj   *graphjin
	op   qcode.QType
	rc   *ReqConfig
	sc   *script
	ns   string
	name string
}

type queryResp struct {
	qc   *queryComp
	data []byte
	role string
}

func (gj *graphjin) initDiscover() error {
	switch gj.conf.DBType {
	case "":
		gj.dbtype = "postgres"
	case "mssql":
		gj.dbtype = "mysql"
	default:
		gj.dbtype = gj.conf.DBType
	}

	if err := gj._initDiscover(); err != nil {
		return fmt.Errorf("%s: %w", gj.dbtype, err)
	}
	return nil
}

func (gj *graphjin) _initDiscover() error {
	var err error

	// If gj.dbinfo is not null then it's probably set
	// for tests
	if gj.dbinfo == nil {
		gj.dbinfo, err = sdata.GetDBInfo(
			gj.db,
			gj.dbtype,
			gj.conf.Blocklist)
	}

	return err
}

func (gj *graphjin) initSchema() error {
	if err := gj._initSchema(); err != nil {
		return fmt.Errorf("%s: %w", gj.dbtype, err)
	}
	return nil
}

func (gj *graphjin) _initSchema() error {
	var err error

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
		if err := addTableInfo(gj.conf, t); err != nil {
			return err
		}
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

	ssufx := gj.conf.SingularSuffix
	if ssufx == "" {
		ssufx = "ByID"
	}
	gj.schema.SingularSuffix = ssufx

	return err
}

func (gj *graphjin) initCompilers() error {
	var err error

	qcc := qcode.Config{
		TConfig:          gj.conf.tmap,
		DefaultBlock:     gj.conf.DefaultBlock,
		DefaultLimit:     gj.conf.DefaultLimit,
		DisableAgg:       gj.conf.DisableAgg,
		DisableFuncs:     gj.conf.DisableFuncs,
		EnableCamelcase:  gj.conf.EnableCamelcase,
		EnableInflection: gj.conf.EnableInflection,
		DBSchema:         gj.schema.DBSchema(),
	}

	if gj.allowList != nil && gj.prod {
		qcc.FragmentFetcher = gj.allowList.FragmentFetcher
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

func (gj *graphjin) executeRoleQuery(c context.Context, conn *sql.Conn, vars []byte, rc *ReqConfig) (string, error) {
	var role string
	var ar args
	var err error

	md := gj.roleStmtMD

	if conn == nil {
		if conn, err = gj.db.Conn(c); err != nil {
			return role, err
		}
		defer conn.Close()
	}

	if c.Value(UserIDKey) == nil {
		return "anon", nil
	}

	if ar, err = gj.argList(c, md, vars, rc); err != nil {
		return "", err
	}

	err = conn.QueryRowContext(c, gj.roleStmt, ar.values...).Scan(&role)
	return role, err
}

func (c *gcontext) execQuery(qr queryReq, role string) (queryResp, error) {
	var res queryResp
	var err error

	err = retry.Do(
		func() error {
			res, err = c.resolveSQL(qr, role)
			return err
		},
		retry.Context(c),
		retry.RetryIf(retryIfDBError),
		retry.Attempts(3),
		retry.LastErrorOnly(true),
	)

	if err != nil {
		return res, err
	}

	if c.gj.conf.Debug {
		c.debugLog(&res.qc.st)
	}

	qc := res.qc.st.qc

	if len(res.data) == 0 {
		return res, nil
	}

	if qc.Remotes != 0 {
		if res, err = c.execRemoteJoin(res); err != nil {
			return res, err
		}
	}

	if c.sc != nil && c.sc.RespFunc != nil {
		res.data, err = c.scriptCallResp(res.data, res.role)
	}

	return res, err
}

func (c *gcontext) resolveSQL(qr queryReq, role string) (queryResp, error) {
	res := queryResp{role: role}

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

	} else if c.gj.abacEnabled {
		res.role, err = c.gj.executeRoleQuery(c, conn, qr.vars, c.rc)
	}

	if err != nil {
		return res, err
	}

	qcomp, err := c.gj.compileQuery(qr, res.role)
	if err != nil {
		return res, err
	}
	res.qc = qcomp

	return c.resolveCompiledQuery(conn, qcomp, res)
}

func (c *gcontext) resolveCompiledQuery(
	conn *sql.Conn,
	qcomp *queryComp,
	res queryResp) (
	queryResp, error) {

	// From here on use qcomp. for everything including accessing qr since it contains updated values of the latter. This code needs some refactoring

	if err := c.validateAndUpdateVars(qcomp, &res); err != nil {
		return res, err
	}

	args, err := c.gj.argList(c, qcomp.st.md, qcomp.qr.vars, c.rc)
	if err != nil {
		return res, err
	}

	// var stime time.Time

	// if c.gj.conf.EnableTracing {
	// 	stime = time.Now()
	// }

	row := conn.QueryRowContext(c, qcomp.st.sql, args.values...)

	if err := row.Scan(&res.data); err == sql.ErrNoRows {
		return res, nil
	} else if err != nil {
		return res, err
	}

	qc := qcomp.st.qc

	cur, err := c.gj.encryptCursor(qc, res.data)
	if err != nil {
		return res, err
	}

	res.data = cur.data

	if !c.gj.prod && c.gj.allowList != nil {
		if err := c.saveToAllowList(
			qc,
			string(qcomp.qr.query),
			qcomp.qr.ns); err != nil {
			return res, err
		}
	}

	// if c.gj.conf.EnableTracing {
	// 	for _, id := range st.qc.Roots {
	// 		c.addTrace(st.qc.Selects, id, stime)
	// 	}
	// }

	return res, nil
}

func (c *gcontext) validateAndUpdateVars(qcomp *queryComp, res *queryResp) error {
	var vars map[string]interface{}
	qc := qcomp.st.qc
	qr := qcomp.qr

	if len(qr.vars) != 0 || qc.Script != "" {
		vars = make(map[string]interface{})
	}

	if len(qr.vars) != 0 {
		if err := json.Unmarshal(qr.vars, &vars); err != nil {
			return err
		}
	}
	if qc.Validation != nil {
		if err := cuejson.Validate(qr.vars, qc.Validation.Cuev); err != nil {
			// TODO: better error handling. it's not clear that error came from validation
			// aslo it needs to be able to parse in frontend,
			// ex: error:{kind:"validation",problem:"out of range",path:"input.id",shoud_be:"<5"}
			return err
		}
	}

	if qc.Consts != nil {
		errs := qcomp.st.va.ValidateMap(vars, qc.Consts)

		if !c.gj.prod && len(errs) != 0 {
			for k, v := range errs {
				v1 := v.(validator.ValidationErrors)
				c.gj.log.Printf("Validation Failed: $%s: %s", k, v1.Error())
			}
		}

		if len(errs) != 0 {
			return errors.New("validation failed")
		}
	}

	if qc.Script != "" {
		if err := c.loadScript(qc.Script); err != nil {
			return err
		}
	}

	if c.sc != nil && c.sc.ReqFunc != nil {
		if v, err := c.scriptCallReq(vars, qcomp.st.role.Name); len(v) != 0 {
			qcomp.qr.vars = v
		} else if err != nil {
			return err
		}
	}
	return nil
}

func (c *gcontext) saveToAllowList(qc *qcode.QCode, query, namespace string) error {
	var av []byte
	var err error

	if v, ok := qc.Vars[qc.ActionVar]; ok {
		av, err = json.Marshal(map[string]json.RawMessage{
			qc.ActionVar: v,
		})
		if err != nil {
			return err
		}
	}

	return c.gj.allowList.Set(av, query, qc.Metadata, namespace)
}

func (c *gcontext) setLocalUserID(conn *sql.Conn) error {
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

func (r *Result) CacheControl() string {
	return r.cacheControl
}

// func (c *gcontext) addTrace(sel []qcode.Select, id int32, st time.Time) {
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

func (c *gcontext) debugLog(st *stmt) {
	for _, sel := range st.qc.Selects {
		if sel.SkipRender == qcode.SkipTypeUserNeeded {
			c.gj.log.Printf("Field skipped, requires $user_id or table not added to anon role: %s", sel.FieldName)
		}
		if sel.SkipRender == qcode.SkipTypeBlocked {
			c.gj.log.Printf("Field skipped, blocked: %s", sel.FieldName)
		}
	}
}

func retryIfDBError(err error) bool {
	return (err == driver.ErrBadConn)
}
