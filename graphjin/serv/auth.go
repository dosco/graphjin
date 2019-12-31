package serv

import (
	"context"
	"net/http"
	"strings"
)

type ctxkey int

const (
	userIDProviderKey ctxkey = iota
	userIDKey
	userRoleKey
)

func headerAuth(next http.Handler) http.HandlerFunc {
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

func withAuth(next http.Handler) http.Handler {
	at := conf.Auth.Type
	ru := conf.Auth.Rails.URL

	if conf.Auth.CredsInHeader {
		next = headerAuth(next)
	}

	switch at {
	case "rails":
		if strings.HasPrefix(ru, "memcache:") {
			return railsMemcacheHandler(next)
		}

		if strings.HasPrefix(ru, "redis:") {
			return railsRedisHandler(next)
		}

		return railsCookieHandler(next)

	case "jwt":
		return jwtHandler(next)
	}

	return next
}
