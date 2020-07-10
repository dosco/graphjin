package serv

import (
	"context"
	"net/http"
)

var healthyResponse = []byte("All's Well")

func health(servConf *ServConfig) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ct, cancel := context.WithTimeout(r.Context(), servConf.conf.DB.PingTimeout)
		defer cancel()

		if err := servConf.db.PingContext(ct); err != nil {
			servConf.log.Printf("ERR error pinging database: %s", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if _, err := w.Write(healthyResponse); err != nil {
			servConf.log.Printf("ERR error writing healthy response: %s", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
}
