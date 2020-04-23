package serv

import (
	"fmt"
	"net/http"
)

type actionFn func(w http.ResponseWriter, r *http.Request) error

func newAction(a *Action) (http.Handler, error) {
	var fn actionFn
	var err error

	if len(a.SQL) != 0 {
		fn, err = newSQLAction(a)
	} else {
		return nil, fmt.Errorf("invalid config for action '%s'", a.Name)
	}

	if err != nil {
		return nil, err
	}

	httpFn := func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			renderErr(w, err)
		}
	}

	return http.HandlerFunc(httpFn), nil
}

func newSQLAction(a *Action) (actionFn, error) {
	fn := func(w http.ResponseWriter, r *http.Request) error {
		_, err := db.ExecContext(r.Context(), a.SQL)
		return err
	}

	return fn, nil
}
