package serv

import (
	"context"
	"net/http"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var healthyResponse = []byte("All's Well")

// healthCheckHandler returns a handler that checks the health of the service
func healthCheckHandler(s1 *HttpService) http.Handler {
	h := func(w http.ResponseWriter, r *http.Request) {
		s := s1.Load().(*graphjinService)
		c, cancel := context.WithTimeout(r.Context(), s.conf.DB.PingTimeout)
		defer cancel()

		c1, span := s.spanStart(c, "Health Check Request")
		defer span.End()

		if err := s.db.PingContext(c1); err != nil {
			spanError(span, err)

			s.zlog.Error("Health Check", []zapcore.Field{zap.Error(err)}...)
			w.WriteHeader(http.StatusInternalServerError)
		}

		_, _ = w.Write(healthyResponse)
	}

	return http.HandlerFunc(h)
}
