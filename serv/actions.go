package serv

import (
	"fmt"
	"net/http"
)

type actionFn func(w http.ResponseWriter, r *http.Request) error

func newAction(s *Service, a *Action) (http.Handler, error) {
	var fn actionFn
	var err error

	if a.SQL != "" {
		fn, err = newSQLAction(s, a)
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

func newSQLAction(s1 *Service, a *Action) (actionFn, error) {
	fn := func(w http.ResponseWriter, r *http.Request) error {
		s := s1.Load().(*service)
		c := r.Context()

		span := s.spanStart(c, "Action Request")
		_, err := s.db.ExecContext(c, a.SQL)
		if err != nil {
			spanError(span, err)
		}
		span.End()

		return err
	}

	return fn, nil
}
