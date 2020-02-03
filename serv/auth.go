package serv

import (
	"context"
	"net/http"
)

type ctxkey int

const (
	userIDProviderKey ctxkey = iota
	userIDKey
	userRoleKey
)

func headerAuth(authc configAuth, next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		userIDProvider := r.Header.Get("X-User-ID-Provider")
		if len(userIDProvider) != 0 {
			ctx = context.WithValue(ctx, userIDProviderKey, userIDProvider)
		}

		userID := r.Header.Get("X-User-ID")
		if len(userID) != 0 {
			ctx = context.WithValue(ctx, userIDKey, userID)
		}

		userRole := r.Header.Get("X-User-Role")
		if len(userRole) != 0 {
			ctx = context.WithValue(ctx, userRoleKey, userRole)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func headerHandler(authc configAuth, next http.Handler) http.HandlerFunc {
	hdr := authc.Header

	if len(hdr.Name) == 0 {
		errlog.Fatal().Str("auth", authc.Name).Msg("no header.name defined")
	}

	if !hdr.Exists && len(hdr.Value) == 0 {
		errlog.Fatal().Str("auth", authc.Name).Msg("no header.value defined")
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
	}
}

func withAuth(next http.Handler, authc configAuth) http.Handler {
	if authc.CredsInHeader {
		next = headerAuth(authc, next)
	}

	switch authc.Type {
	case "rails":
		return railsHandler(authc, next)

	case "jwt":
		return jwtHandler(authc, next)

	case "header":
		return headerHandler(authc, next)

	}

	return next
}
