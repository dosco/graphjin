package serv

import (
	"context"
	"net/http"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var healthyResponse = []byte("All's Well")

// defaultHealthPingTimeout bounds the health probe when no ping_timeout is
// configured. In sources mode the legacy DB block is never populated, so
// conf.DB.PingTimeout is zero — without this guard the probe context is
// born expired and /health returns 500 (context deadline exceeded) while
// queries serve fine.
const defaultHealthPingTimeout = 5 * time.Second

// healthCheckHandler returns a handler that checks the health of the service
func healthCheckHandler(s1 *HttpService) http.Handler {
	h := func(w http.ResponseWriter, r *http.Request) {
		s := s1.Load().(*graphjinService)
		pingTimeout := s.conf.DB.PingTimeout
		if pingTimeout <= 0 {
			pingTimeout = defaultHealthPingTimeout
		}
		c, cancel := context.WithTimeout(r.Context(), pingTimeout)
		defer cancel()

		c1, span := s.spanStart(c, "Health Check Request")
		defer span.End()

		db := s.anyDB()
		if db == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("no database connection"))
			return
		}
		if err := db.PingContext(c1); err != nil {
			spanError(span, err)

			s.zlog.Error("Health Check", []zapcore.Field{zap.Error(err)}...)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("database ping failed"))
			return
		}

		_, _ = w.Write(healthyResponse)
	}

	return http.HandlerFunc(h)
}
