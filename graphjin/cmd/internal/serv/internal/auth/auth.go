package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/dosco/super-graph/config"
	"github.com/dosco/super-graph/core"
)

func SimpleHandler(ac *config.Auth, next http.Handler) (http.HandlerFunc, error) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		userIDProvider := r.Header.Get("X-User-ID-Provider")
		if len(userIDProvider) != 0 {
			ctx = context.WithValue(ctx, core.UserIDProviderKey, userIDProvider)
		}

		userID := r.Header.Get("X-User-ID")
		if len(userID) != 0 {
			ctx = context.WithValue(ctx, core.UserIDKey, userID)
		}

		userRole := r.Header.Get("X-User-Role")
		if len(userRole) != 0 {
			ctx = context.WithValue(ctx, core.UserRoleKey, userRole)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	}, nil
}

func HeaderHandler(ac *config.Auth, next http.Handler) (http.HandlerFunc, error) {
	hdr := ac.Header

	if len(hdr.Name) == 0 {
		return nil, fmt.Errorf("auth '%s': no header.name defined", ac.Name)
	}

	if !hdr.Exists && len(hdr.Value) == 0 {
		return nil, fmt.Errorf("auth '%s': no header.value defined", ac.Name)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		var fo1 bool
		value := r.Header.Get(hdr.Name)

		switch {
		case hdr.Exists:
			fo1 = (len(value) == 0)

		default:
			fo1 = (value != hdr.Value)
		}

		if fo1 {
			http.Error(w, "401 unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	}, nil
}

func WithAuth(next http.Handler, ac *config.Auth) (http.Handler, error) {
	var err error

	if ac.CredsInHeader {
		next, err = SimpleHandler(ac, next)
	}

	if err != nil {
		return nil, err
	}

	switch ac.Type {
	case "rails":
		return RailsHandler(ac, next)

	case "jwt":
		return JwtHandler(ac, next)

	case "header":
		return HeaderHandler(ac, next)

	}

	return next, nil
}

func IsAuth(ct context.Context) bool {
	return ct.Value(core.UserIDKey) != nil
}
