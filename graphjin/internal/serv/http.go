package serv

import (
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/dosco/super-graph/core"
	"github.com/dosco/super-graph/internal/serv/internal/auth"
	"github.com/rs/cors"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/trace"
	"go.uber.org/zap"
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

func apiV1Handler() http.Handler {
	h, err := auth.WithAuth(http.HandlerFunc(apiV1), &conf.Auth)
	if err != nil {
		log.Fatalf("ERR %s", err)
	}

	if len(conf.AllowedOrigins) != 0 {
		c := cors.New(cors.Options{
			AllowedOrigins:   conf.AllowedOrigins,
			AllowCredentials: true,
			Debug:            conf.DebugCORS,
		})
		return c.Handler(h)
	}

	return h
}

func apiV1(w http.ResponseWriter, r *http.Request) {
	ct := r.Context()
	w.Header().Set("Content-Type", "application/json")

	//nolint: errcheck
	if conf.AuthFailBlock && !auth.IsAuth(ct) {
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

	err = json.Unmarshal(b, &req)
	if err != nil {
		renderErr(w, err)
		return
	}

	doLog := true
	res, err := sg.GraphQL(ct, req.Query, req.Vars)

	if conf.telemetryEnabled() {
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

	if !conf.Production && res.QueryName() == introspectionQuery {
		doLog = false
	}

	if doLog && logLevel >= LogLevelDebug {
		log.Printf("DBG query %s: %s", res.QueryName(), res.SQL())
	}

	if err == nil {
		if conf.CacheControl != "" && res.Operation() == core.OpQuery {
			w.Header().Set("Cache-Control", conf.CacheControl)
		}
		//nolint: errcheck
		json.NewEncoder(w).Encode(res)

		if doLog && logLevel >= LogLevelInfo {
			zlog.Info("success",
				zap.String("op", res.OperationName()),
				zap.String("name", res.QueryName()),
				zap.String("role", res.Role()),
			)
		}

	} else {
		renderErr(w, err)

		if doLog && logLevel >= LogLevelInfo {
			zlog.Error("error",
				zap.String("op", res.OperationName()),
				zap.String("name", res.QueryName()),
				zap.String("role", res.Role()),
				zap.Error(err),
			)
		}

	}

}

//nolint: errcheck
func renderErr(w http.ResponseWriter, err error) {
	if err == errUnauthorized {
		w.WriteHeader(http.StatusUnauthorized)
	}

	json.NewEncoder(w).Encode(errorResp{err.Error()})
}
