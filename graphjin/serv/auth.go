package serv

import (
	"context"
	"errors"
	"net/http"
)

const (
	salt        = "encrypted cookie"
	signSalt    = "signed encrypted cookie"
	emptySecret = ""
	authHeader  = "Authorization"
)

var (
	userIDProviderKey = struct{}{}
	userIDKey         = struct{}{}
	errSessionData    = errors.New("error decoding session data")
)

func headerHandler(next http.HandlerFunc) http.HandlerFunc {
	fn := conf.Auth.Header
	if len(fn) == 0 {
		panic(errors.New("no auth.header defined"))
	}

	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get(fn)
		if len(userID) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func withAuth(next http.HandlerFunc) http.HandlerFunc {
	at := conf.Auth.Type

	switch at {
	case "header":
		return headerHandler(next)

	case "rails_cookie":
		return railsCookieHandler(next)

	case "rails_memcache":
		return railsMemcacheHandler(next)

	case "rails_redis":
		return railsRedisHandler(next)

	case "jwt":
		return jwtHandler(next)

	default:
		return next
	}

	return next
}
