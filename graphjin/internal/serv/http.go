package serv

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/dosco/graphjin/core"
	"github.com/dosco/graphjin/internal/serv/internal/auth"
	"github.com/dosco/graphjin/internal/serv/internal/etags"
	"github.com/gorilla/websocket"
	"github.com/rs/cors"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	maxReadBytes = 100000 // 100Kb
)

var (
	errUnauthorized = errors.New("not authorized")
)

type extensions struct {
	Persisted apqExt `json:"persistedQuery"`
}

type apqExt struct {
	Version    int    `json:"version"`
	Sha256Hash string `json:"sha256Hash"`
}

type gqlReq struct {
	OpName string          `json:"operationName"`
	Query  string          `json:"query"`
	Vars   json.RawMessage `json:"variables"`
	Ext    extensions      `json:"extensions"`
}

type errorResp struct {
	Errors []string `json:"errors"`
}

func apiV1Handler(sc *ServConfig) http.Handler {
	h, err := auth.WithAuth(http.HandlerFunc(sc.apiV1()), &sc.conf.Auth)
	if err != nil {
		sc.log.Fatalf("Error initializing auth: %s", err)
	}

	if len(sc.conf.AllowedOrigins) != 0 {
		allowedHeaders := []string{
			"Origin", "Accept", "Content-Type", "X-Requested-With", "Authorization"}

		if len(sc.conf.AllowedHeaders) != 0 {
			allowedHeaders = sc.conf.AllowedHeaders
		}

		c := cors.New(cors.Options{
			AllowedOrigins:   sc.conf.AllowedOrigins,
			AllowedHeaders:   allowedHeaders,
			AllowCredentials: true,
			Debug:            sc.conf.DebugCORS,
		})
		h = c.Handler(h)
	}

	h = etags.Handler(h, false)
	return h
}

func (sc *ServConfig) apiV1() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var err error

		if websocket.IsWebSocketUpgrade(r) {
			sc.apiV1Ws(w, r)
			return
		}

		ct := r.Context()
		w.Header().Set("Content-Type", "application/json")

		//nolint: errcheck
		if sc.conf.AuthFailBlock && !auth.IsAuth(ct) {
			renderErr(w, errUnauthorized)
			return
		}

		req := gqlReq{}

		switch r.Method {
		case "POST":
			var b []byte
			b, err = ioutil.ReadAll(io.LimitReader(r.Body, maxReadBytes))
			if err == nil {
				defer r.Body.Close()
				err = json.Unmarshal(b, &req)
			}

		case "GET":
			q := r.URL.Query()
			req.Query = q.Get("query")
			req.OpName = q.Get("operationName")
			req.Vars = json.RawMessage(q.Get("variables"))

			if ext := q.Get("extensions"); ext != "" {
				err = json.Unmarshal([]byte(ext), &req.Ext)
			}
		}

		if err != nil {
			renderErr(w, err)
			return
		}

		rc := core.ReqConfig{Vars: make(map[string]interface{})}

		for k, v := range sc.conf.HeaderVars {
			rc.Vars[k] = func() string {
				if v1, ok := r.Header[v]; ok {
					return v1[0]
				}
				return ""
			}
		}

		switch {
		case gj.IsProd():
			rc.APQKey = req.OpName
		case req.apqEnabled():
			rc.APQKey = (req.OpName + req.Ext.Persisted.Sha256Hash)
		}

		res, err := gj.GraphQL(ct, req.Query, req.Vars, &rc)

		if err == nil &&
			r.Method == "GET" &&
			sc.conf.CacheControl != "" &&
			res.Operation() == core.OpQuery {
			w.Header().Set("Cache-Control", sc.conf.CacheControl)
		}

		if err := json.NewEncoder(w).Encode(res); err != nil {
			renderErr(w, err)
			return
		}

		if sc.conf.telemetryEnabled() {
			span := trace.FromContext(ct)

			span.AddAttributes(
				trace.StringAttribute("operation", res.OperationName()),
				trace.StringAttribute("query_name", res.QueryName()),
				trace.StringAttribute("role", res.Role()),
			)
			if err != nil {
				span.AddAttributes(trace.StringAttribute("error", err.Error()))
			}
			ochttp.SetRoute(ct, apiRoute)
		}

		if sc.logLevel >= LogLevelInfo {
			sc.reqLog(res, err)
		}
	}
}

func (sc *ServConfig) reqLog(res *core.Result, err error) {
	fields := []zapcore.Field{
		zap.String("op", res.OperationName()),
		zap.String("name", res.QueryName()),
		zap.String("role", res.Role()),
	}

	if sc.logLevel >= LogLevelDebug {
		fields = append(fields, zap.String("sql", res.SQL()))
	}

	if err != nil {
		fields = append(fields, zap.Error(err))
		sc.zlog.Error("Query Failed", fields...)
	} else {
		sc.zlog.Info("Query", fields...)
	}
}

func (r gqlReq) apqEnabled() bool {
	return r.Ext.Persisted.Sha256Hash != ""
}

//nolint: errcheck
func renderErr(w http.ResponseWriter, err error) {
	if err == errUnauthorized {
		w.WriteHeader(http.StatusUnauthorized)
	}

	err1 := json.NewEncoder(w).Encode(errorResp{[]string{err.Error()}})
	if err1 != nil {
		panic(fmt.Errorf("%s: %w", err, err1))
	}
}
