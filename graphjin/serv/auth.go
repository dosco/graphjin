package serv

import (
	"context"
	"net/http"
	"strings"
)

var (
	userIDProviderKey = struct{}{}
	userIDKey         = struct{}{}
)

func headerAuth(r *http.Request, c *config) *http.Request {
	if len(c.Auth.Header) == 0 {
		return nil
	}

	userID := r.Header.Get(c.Auth.Header)
	if len(userID) != 0 {
		ctx := context.WithValue(r.Context(), userIDKey, userID)
		return r.WithContext(ctx)
	}

	return nil
}

func withAuth(next http.HandlerFunc) http.HandlerFunc {
	at := conf.Auth.Type
	ru := conf.Auth.Rails.URL

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
