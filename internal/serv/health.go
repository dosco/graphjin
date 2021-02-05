package serv

import (
	"context"
	"net/http"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var healthyResponse = []byte("All's Well")

func health(servConf *ServConfig) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ct, cancel := context.WithTimeout(r.Context(), servConf.conf.DB.PingTimeout)
		defer cancel()

		err := servConf.db.PingContext(ct)
		if err != nil {
			servConf.zlog.Error("Health check failed", []zapcore.Field{zap.Error(err)}...)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		_, _ = w.Write(healthyResponse)
	}
}
