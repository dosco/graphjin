package serv

import (
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/dosco/super-graph/core"
	"github.com/dosco/super-graph/internal/serv/internal/auth"
	"github.com/gorilla/websocket"
	"github.com/rs/cors"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	maxReadBytes       = 100000 // 100Kb
	introspectionQuery = "IntrospectionQuery"
)

var (
	errUnauthorized = errors.New("not authorized")
)

type gqlReq struct {
	OpName string          `json:"operationName"`
	Query  string          `json:"query"`
	Vars   json.RawMessage `json:"variables"`
}

type errorResp struct {
	Error string `json:"error"`
}

func apiV1Handler(servConf *ServConfig) http.Handler {
	h, err := auth.WithAuth(http.HandlerFunc(apiV1(servConf)), &servConf.conf.Auth)
	if err != nil {
		servConf.log.Fatalf("ERR %s", err)
	}

	if len(servConf.conf.AllowedOrigins) != 0 {
		c := cors.New(cors.Options{
			AllowedOrigins:   servConf.conf.AllowedOrigins,
			AllowCredentials: true,
			Debug:            servConf.conf.DebugCORS,
		})
		return c.Handler(h)
	}

	return h
}

func apiV1(servConf *ServConfig) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if websocket.IsWebSocketUpgrade(r) {
			apiV1Ws(servConf, w, r)
			return
		}

		ct := r.Context()
		w.Header().Set("Content-Type", "application/json")

		//nolint: errcheck
		if servConf.conf.AuthFailBlock && !auth.IsAuth(ct) {
			renderErr(w, errUnauthorized)
			return
		}

		b, err := ioutil.ReadAll(io.LimitReader(r.Body, maxReadBytes))
		if err != nil {
			renderErr(w, err)
			return
		}
		defer r.Body.Close()

		req := gqlReq{}

		if err = json.Unmarshal(b, &req); err != nil {
			renderErr(w, err)
			return
		}

		doLog := true
		res, err := sg.GraphQL(ct, req.Query, req.Vars)

		if servConf.conf.telemetryEnabled() {
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

		if !servConf.conf.Production && res.QueryName() == introspectionQuery {
			doLog = false
		}

		if doLog && servConf.logLevel >= LogLevelDebug {
			servConf.log.Printf("DBG query %s: %s", res.QueryName(), res.SQL())
		}

		if err == nil {
			if servConf.conf.CacheControl != "" && res.Operation() == core.OpQuery {
				w.Header().Set("Cache-Control", servConf.conf.CacheControl)
			}
			//nolint: errcheck
			json.NewEncoder(w).Encode(res)

		} else {
			renderErr(w, err)
		}

		if doLog && servConf.logLevel >= LogLevelInfo {
			reqLog(servConf, res, err)
		}
	}
}

func reqLog(servConf *ServConfig, res *core.Result, err error) {
	var msg string

	fields := []zapcore.Field{
		zap.String("op", res.OperationName()),
		zap.String("name", res.QueryName()),
		zap.String("role", res.Role()),
	}

	if err != nil {
		msg = "error"
		fields = append(fields, zap.Error(err))
	} else {
		msg = "success"
	}

	servConf.zlog.Info(msg, fields...)
}

//nolint: errcheck
func renderErr(w http.ResponseWriter, err error) {
	if err == errUnauthorized {
		w.WriteHeader(http.StatusUnauthorized)
	}

	json.NewEncoder(w).Encode(errorResp{err.Error()})
}
