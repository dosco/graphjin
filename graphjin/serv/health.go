package serv

import (
	"context"
	"net/http"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var healthyResponse = []byte("All's Well")

func healthV1Handler(s1 *Service) http.Handler {
	h := func(w http.ResponseWriter, r *http.Request) {
		s := s1.Load().(*service)
		ct, cancel := context.WithTimeout(r.Context(), s.conf.DB.PingTimeout)
		defer cancel()

		span := s.spanStart(ct, "Health Check Request")
		err := s.db.PingContext(ct)
		if err != nil {
			spanError(span, err)
		}
		span.End()

		if err != nil {
			s.zlog.Error("Health Check", []zapcore.Field{zap.Error(err)}...)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		_, _ = w.Write(healthyResponse)
	}

	return http.HandlerFunc(h)
}
