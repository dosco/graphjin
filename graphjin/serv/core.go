package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-pg/pg"
	"github.com/valyala/fasttemplate"
)

func handleReq(ctx context.Context, w io.Writer, req *gqlReq) error {
	qc, err := qcompile.CompileQuery(req.Query)
	if err != nil {
		return err
	}

	var sqlStmt strings.Builder

	if err := pcompile.Compile(&sqlStmt, qc); err != nil {
		return err
	}

	t := fasttemplate.New(sqlStmt.String(), openVar, closeVar)
	sqlStmt.Reset()

	_, err = t.Execute(&sqlStmt, varMap(ctx, req.Vars))

	if err == errNoUserID &&
		authFailBlock == authFailBlockPerQuery &&
		authCheck(ctx) == false {
		return errUnauthorized
	}

	if err != nil {
		return err
	}

	finalSQL := sqlStmt.String()
	if conf.DebugLevel > 0 {
		fmt.Println(finalSQL)
	}
	st := time.Now()

	var root json.RawMessage
	_, err = db.Query(pg.Scan(&root), finalSQL)

	if err != nil {
		return err
	}

	et := time.Now()
	resp := gqlResp{Data: json.RawMessage(root)}

	if conf.EnableTracing {
		resp.Extensions = &extensions{newTrace(st, et, qc)}
	}

	json.NewEncoder(w).Encode(resp)
	return nil
}
