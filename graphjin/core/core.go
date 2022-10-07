package core

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	cuejson "cuelang.org/go/encoding/json"
	"github.com/avast/retry-go"
	"github.com/dosco/graphjin/core/internal/psql"
	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/dosco/graphjin/core/internal/sdata"
	"github.com/go-playground/validator/v10"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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

type gcontext struct {
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

	if err != nil {
		return err
	}

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
		FragmentFetcher:  gj.allowList.FragmentFetcher,
	}

	gj.qc, err = qcode.NewCompiler(gj.schema, qcc)
	if err != nil {
		return err
	}

	if err := addRoles(gj.conf, gj.qc); err != nil {
		return err
	}

	gj.pc = psql.NewCompiler(psql.Config{
		Vars:            gj.conf.Vars,
		DBType:          gj.schema.DBType(),
		DBVersion:       gj.schema.DBVersion(),
		EnableCamelcase: gj.conf.EnableCamelcase,
	})
	return nil
}

func (gj *graphjin) executeRoleQuery(ctx context.Context, conn *sql.Conn, vars []byte, rc *ReqConfig) (string, error) {
	var role string
	var ar args
	var err error

	md := gj.roleStmtMD

	if ctx.Value(UserIDKey) == nil {
		return "anon", nil
	}

	if ar, err = gj.argList(ctx, md, vars, rc); err != nil {
		return "", err
	}

	if conn == nil {
		ctx1, span := gj.spanStart(ctx, "Get Connection")
		err = retryOperation(ctx1, func() error {
			conn, err = gj.db.Conn(ctx1)
			return err
		})
		if err != nil {
			spanError(span, err)
		}
		span.End()

		if err != nil {
			return role, err
		}
		defer conn.Close()
	}

	ctx1, span := gj.spanStart(ctx, "Execute Role Query")
	defer span.End()

	err = retryOperation(ctx1, func() error {
		return conn.
			QueryRowContext(ctx1, gj.roleStmt, ar.values...).
			Scan(&role)
	})

	if err != nil {
		spanError(span, err)
		return role, err
	}

	if span.IsRecording() {
		span.SetAttributes(attribute.String("role", role))
	}

	return role, err
}

func (c *gcontext) execQuery(ctx context.Context, qr queryReq, role string) (queryResp, error) {
	var res queryResp
	var err error

	if res, err = c.resolveSQL(ctx, qr, role); err != nil {
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
		if res, err = c.execRemoteJoin(ctx, res); err != nil {
			return res, err
		}
	}

	if c.sc != nil && c.sc.RespFunc != nil {
		res.data, err = c.scriptCallResp(ctx, res.data, res.role)
	}

	return res, err
}

func (c *gcontext) resolveSQL(ctx context.Context, qr queryReq, role string) (queryResp, error) {
	var conn *sql.Conn
	var err error

	res := queryResp{role: role}

	ctx1, span := c.gj.spanStart(ctx, "Get Connection")
	err = retryOperation(ctx1, func() error {
		conn, err = c.gj.db.Conn(ctx1)
		return err
	})
	if err != nil {
		spanError(span, err)
	}
	span.End()

	if err != nil {
		return res, err
	}
	defer conn.Close()

	if c.gj.conf.SetUserID {
		ctx1, span = c.gj.spanStart(ctx, "Set Local User ID")
		err = retryOperation(ctx1, func() error {
			return c.setLocalUserID(ctx1, conn)
		})
		if err != nil {
			spanError(span, err)
		}
		span.End()

		if err != nil {
			return res, err
		}
	}

	if v := ctx.Value(UserRoleKey); v != nil {
		res.role = v.(string)
	} else if c.gj.abacEnabled {
		res.role, err = c.gj.executeRoleQuery(ctx, conn, qr.vars, c.rc)
	}

	if err != nil {
		return res, err
	}

	qcomp, err := c.gj.compileQuery(qr, res.role)
	if err != nil {
		return res, err
	}
	res.qc = qcomp

	return c.resolveCompiledQuery(ctx, conn, qcomp, res)
}

func (c *gcontext) resolveCompiledQuery(
	ctx context.Context,
	conn *sql.Conn,
	qcomp *queryComp,
	res queryResp) (
	queryResp, error) {

	// From here on use qcomp. for everything including accessing qr since it contains updated values of the latter. This code needs some refactoring

	if err := c.validateAndUpdateVars(ctx, qcomp, &res); err != nil {
		return res, err
	}

	args, err := c.gj.argList(ctx, qcomp.st.md, qcomp.qr.vars, c.rc)
	if err != nil {
		return res, err
	}

	ctx1, span := c.gj.spanStart(ctx, "Execute Query")
	defer span.End()

	err = retryOperation(ctx1, func() error {
		return conn.
			QueryRowContext(ctx1, qcomp.st.sql, args.values...).
			Scan(&res.data)
	})

	if err != nil && err != sql.ErrNoRows {
		spanError(span, err)
	}

	if span.IsRecording() {
		op := qcomp.st.qc.Type.String()
		span.SetAttributes(
			attribute.String("query.namespace", res.qc.qr.ns),
			attribute.String("query.operation", op),
			attribute.String("query.name", qcomp.st.qc.Name),
			attribute.String("query.role", qcomp.st.role.Name))
	}

	if err == sql.ErrNoRows {
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
	return res, nil
}

func (c *gcontext) validateAndUpdateVars(ctx context.Context, qcomp *queryComp, res *queryResp) error {
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
		if v, err := c.scriptCallReq(ctx, vars, qcomp.st.role.Name); len(v) != 0 {
			qcomp.qr.vars = v
		} else if err != nil {
			return err
		}
	}
	return nil
}

func (c *gcontext) setLocalUserID(ctx context.Context, conn *sql.Conn) error {
	var err error

	if v := ctx.Value(UserIDKey); v == nil {
		return nil
	} else {
		switch v1 := v.(type) {
		case string:
			_, err = conn.ExecContext(ctx, `SET SESSION "user.id" = '`+v1+`'`)

		case int:
			_, err = conn.ExecContext(ctx, `SET SESSION "user.id" = `+strconv.Itoa(v1))
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

func (gj *graphjin) saveToAllowList(qc *qcode.QCode, query, namespace string) error {
	var av []byte
	var err error

	if gj.conf.DisableAllowList {
		return nil
	}

	if v, ok := qc.Vars[qc.ActionVar]; ok {
		av, err = json.Marshal(map[string]json.RawMessage{
			qc.ActionVar: v,
		})
		if err != nil {
			return err
		}
	}

	return gj.allowList.Set(av, query, qc.Metadata, namespace)
}

func (gj *graphjin) spanStart(c context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return gj.tracer.Start(c, name, opts...)
}

func spanError(span trace.Span, err error) {
	if span.IsRecording() {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

func retryOperation(c context.Context, fn func() error) error {
	return retry.Do(
		fn,
		retry.Context(c),
		retry.RetryIf(retryIfDBError),
		retry.Attempts(3),
		retry.LastErrorOnly(true),
	)
}
