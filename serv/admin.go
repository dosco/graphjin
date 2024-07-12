package serv

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sync/atomic"
	"time"
)

// adminDeployHandler handles the admin deploy endpoint
func adminDeployHandler(s1 *HttpService) http.Handler {
	h := func(w http.ResponseWriter, r *http.Request) {
		var req DeployReq
		s := s1.Load().(*graphjinService)

		if !s.isAdminSecret(r) {
			authFail(w)
			return
		}

		de := json.NewDecoder(r.Body)
		if err := de.Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Name == "" {
			badReq(w, "name is a required field")
			return
		}

		if req.Bundle == "" {
			badReq(w, "bundle is a required field")
			return
		}

		res, err := s.saveConfig(r.Context(), req.Name, req.Bundle)
		if err != nil {
			intErr(w, fmt.Sprintf("deploy error: %s", err.Error()))
			return
		}
		var msg string

		if res.name != res.pname && res.pname != "" {
			msg = fmt.Sprintf("deploy successful: '%s', replacing: '%s'", res.name, res.pname)
		} else {
			msg = fmt.Sprintf("deploy successful: '%s'", res.name)
		}

		_, _ = io.WriteString(w, msg)
	}

	return http.HandlerFunc(h)
}

// adminRollbackHandler handles the admin rollback endpoint
func adminRollbackHandler(s1 *HttpService) http.Handler {
	h := func(w http.ResponseWriter, r *http.Request) {
		s := s1.Load().(*graphjinService)

		if !s.isAdminSecret(r) {
			authFail(w)
			return
		}

		res, err := s.rollbackConfig(r.Context())
		if err != nil {
			intErr(w, fmt.Sprintf("error rolling-back config: %s", err.Error()))
			return
		}

		var msg string

		if res.name != res.pname && res.name != "" {
			msg = fmt.Sprintf("rollback successful: '%s', replacing: '%s'", res.pname, res.name)
		} else {
			msg = fmt.Sprintf("rollback successful: '%s'", res.pname)
		}
		_, _ = io.WriteString(w, msg)
	}

	return http.HandlerFunc(h)
}

// adminConfigHandler handles the checking of the admin secret endpoint
func (s *graphjinService) isAdminSecret(r *http.Request) bool {
	atomic.AddInt32(&s.adminCount, 1)
	defer atomic.StoreInt32(&s.adminCount, 0)

	//#nosec G404
	time.Sleep(time.Duration(rand.Intn(4000-2000)+2000) * time.Millisecond)

	if s.adminCount > 2 {
		return false
	}

	hv := r.Header.Get("Authorization")
	if hv == "" {
		return false
	}

	v1, err := base64.StdEncoding.DecodeString(hv[7:])
	return (err == nil) && bytes.Equal(v1, s.asec[:])
}

// badReq sends a bad request response
func badReq(w http.ResponseWriter, msg string) {
	http.Error(w, msg, http.StatusBadRequest)
}

// intErr sends an internal server error response
func intErr(w http.ResponseWriter, msg string) {
	http.Error(w, msg, http.StatusInternalServerError)
}

// authFail sends an unauthorized response
func authFail(w http.ResponseWriter) {
	http.Error(w, "auth failed", http.StatusUnauthorized)
}
