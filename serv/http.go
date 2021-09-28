package serv

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"

	"github.com/dosco/graphjin/core"
	"github.com/dosco/graphjin/serv/internal/auth"
	"github.com/dosco/graphjin/serv/internal/etags"
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

func apiV1Handler(s1 *Service) http.Handler {
	var zlog *zap.Logger
	s := s1.Load().(*service)

	if s.conf.Debug {
		zlog = s.zlog
	}

	h, err := auth.WithAuth(s1.apiV1(), &s.conf.Auth, zlog)
	if err != nil {
		s.log.Fatalf("Error initializing auth: %s", err)
	}

	if len(s.conf.AllowedOrigins) != 0 {
		allowedHeaders := []string{
			"Origin", "Accept", "Content-Type", "X-Requested-With", "Authorization"}

		if len(s.conf.AllowedHeaders) != 0 {
			allowedHeaders = s.conf.AllowedHeaders
		}

		c := cors.New(cors.Options{
			AllowedOrigins:   s.conf.AllowedOrigins,
			AllowedHeaders:   allowedHeaders,
			AllowCredentials: true,
			Debug:            s.conf.DebugCORS,
		})
		h = c.Handler(h)
	}

	h = etags.Handler(h, false)
	return h
}

func (s1 *Service) apiV1() http.Handler {
	h := func(w http.ResponseWriter, r *http.Request) {
		var err error
		s := s1.Load().(*service)
		start := time.Now()

		if websocket.IsWebSocketUpgrade(r) {
			s.apiV1Ws(w, r)
			return
		}

		ct := r.Context()
		w.Header().Set("Content-Type", "application/json")

		//nolint: errcheck
		if s.conf.AuthFailBlock && !auth.IsAuth(ct) {
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

		for k, v := range s.conf.HeaderVars {
			rc.Vars[k] = func() string {
				if v1, ok := r.Header[v]; ok {
					return v1[0]
				}
				return ""
			}
		}

		switch {
		case s.gj.IsProd():
			rc.APQKey = req.OpName
		case req.apqEnabled():
			rc.APQKey = (req.OpName + req.Ext.Persisted.Sha256Hash)
		}

		if req.OpName == "subscription" {
			renderErr(w, errors.New("use websockets for subscriptions"))
			return
		}

		res, err := s.gj.GraphQL(ct, req.Query, req.Vars, &rc)

		if err == nil && r.Method == "GET" && res.Operation() == core.OpQuery {
			switch {
			case res.CacheControl() != "":
				w.Header().Set("Cache-Control", res.CacheControl())

			case s.conf.CacheControl != "":
				w.Header().Set("Cache-Control", s.conf.CacheControl)
			}
		}

		if err := json.NewEncoder(w).Encode(res); err != nil {
			renderErr(w, err)
			return
		}

		if s.conf.telemetryEnabled() {
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

		elapsed := time.Since(start).Milliseconds()

		if res != nil {
			res.ResponseTime = elapsed
		}

		if s.logLevel >= logLevelInfo {
			s.reqLog(res, err)
		}
	}

	return http.HandlerFunc(h)
}

func (s *service) reqLog(res *core.Result, err error) {
	fields := []zapcore.Field{
		zap.String("op", res.OperationName()),
		zap.String("name", res.QueryName()),
		zap.String("role", res.Role()),
	}

	if s.logLevel >= logLevelDebug {
		fields = append(fields, zap.String("sql", res.SQL()))
	}

	if s.conf.Debug {
		s.log.Info(res.SQL())
	}

	if err != nil {
		fields = append(fields, zap.Error(err))
		s.zlog.Error("Query Failed", fields...)
	} else {
		fields = append(fields, zap.String("responseTime", strconv.FormatInt(res.ResponseTime, 10)+"ms"))
		s.zlog.Info("Query", fields...)
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
