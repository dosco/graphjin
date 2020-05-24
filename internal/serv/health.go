package serv

import (
	"context"
	"net/http"
)

var healthyResponse = []byte("All's Well")

func health(w http.ResponseWriter, r *http.Request) {
	ct, cancel := context.WithTimeout(r.Context(), conf.DB.PingTimeout)
	defer cancel()

	if err := db.PingContext(ct); err != nil {
		log.Printf("ERR error pinging database: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if _, err := w.Write(healthyResponse); err != nil {
		log.Printf("ERR error writing healthy response: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}
