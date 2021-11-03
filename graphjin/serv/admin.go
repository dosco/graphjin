//nolint:errcheck
package serv

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/dosco/graphjin/internal/common"
)

func adminDeployHandler(s1 *Service) http.Handler {
	h := func(w http.ResponseWriter, r *http.Request) {
		var req common.DeployReq
		s := s1.Load().(*service)

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

		io.WriteString(w, msg)
	}

	return http.HandlerFunc(h)
}

func adminRollbackHandler(s1 *Service) http.Handler {
	h := func(w http.ResponseWriter, r *http.Request) {
		s := s1.Load().(*service)

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
		io.WriteString(w, msg)
	}

	return http.HandlerFunc(h)
}

func (s *service) isAdminSecret(r *http.Request) bool {
	time.Sleep(3 * time.Second)

	hv := r.Header["Authorization"]
	if len(hv) == 0 || len(hv[0]) < 10 {
		return false
	}

	v1, err := base64.StdEncoding.DecodeString(hv[0][7:])
	return (err == nil) && bytes.Equal(v1, s.asec[:])
}

func badReq(w http.ResponseWriter, msg string) {
	http.Error(w, msg, http.StatusBadRequest)
}

func intErr(w http.ResponseWriter, msg string) {
	http.Error(w, msg, http.StatusInternalServerError)
}

func authFail(w http.ResponseWriter) {
	http.Error(w, "auth failed", http.StatusUnauthorized)
}
