package serv

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/allegro/bigcache"
	"github.com/dosco/super-graph/qcode"
	"github.com/go-pg/pg"
	"github.com/valyala/fasttemplate"
)

var (
	cache, _ = bigcache.NewBigCache(bigcache.DefaultConfig(24 * time.Hour))
)

func handleReq(ctx context.Context, w io.Writer, req *gqlReq) error {
	var key, finalSQL string
	var qc *qcode.QCode

	var entry []byte
	var err error

	cacheEnabled := (conf.EnableTracing == false)

	if cacheEnabled {
		k := sha1.Sum([]byte(req.Query))
		key = string(k[:])
		entry, err = cache.Get(key)
	}

	if len(entry) == 0 || err == bigcache.ErrEntryNotFound {
		qc, err = qcompile.CompileQuery(req.Query)
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

		finalSQL = sqlStmt.String()

	} else if err != nil {
		return err

	} else {
		finalSQL = string(entry)
	}

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

	if cacheEnabled {
		if err = cache.Set(key, []byte(finalSQL)); err != nil {
			return err
		}
	}

	if conf.EnableTracing {
		resp.Extensions = &extensions{newTrace(st, et, qc)}
	}

	json.NewEncoder(w).Encode(resp)
	return nil
}
