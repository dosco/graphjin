package serv

import (
	"context"
	"errors"
	"net/http"
	"strings"
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
	fn := conf.GetString("auth.field_name")
	if len(fn) == 0 {
		panic(errors.New("no auth.field_name defined"))
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
	atype := strings.ToLower(conf.GetString("auth.type"))
	if len(atype) == 0 {
		return next
	}
	store := strings.ToLower(conf.GetString("auth.store"))

	switch atype {
	case "header":
		return headerHandler(next)

	case "rails":
		switch store {
		case "memcache":
			return railsMemcacheHandler(next)

		case "redis":
			return railsRedisHandler(next)

		default:
			return railsCookieHandler(next)
		}

	case "jwt":
		return jwtHandler(next)

	default:
		panic(errors.New("unknown auth.type"))
	}

	return next
}
