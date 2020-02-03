package serv

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/dosco/super-graph/rails"
	"github.com/garyburd/redigo/redis"
)

func railsHandler(authc configAuth, next http.Handler) http.HandlerFunc {
	ru := authc.Rails.URL

	if strings.HasPrefix(ru, "memcache:") {
		return railsMemcacheHandler(authc, next)
	}

	if strings.HasPrefix(ru, "redis:") {
		return railsRedisHandler(authc, next)
	}

	return railsCookieHandler(authc, next)
}

func railsRedisHandler(authc configAuth, next http.Handler) http.HandlerFunc {
	cookie := authc.Cookie
	if len(cookie) == 0 {
		errlog.Fatal().Msg("no auth.cookie defined")
	}

	if len(authc.Rails.URL) == 0 {
		errlog.Fatal().Msg("no auth.rails.url defined")
	}

	rp := &redis.Pool{
		MaxIdle:   authc.Rails.MaxIdle,
		MaxActive: authc.Rails.MaxActive,
		Dial: func() (redis.Conn, error) {
			c, err := redis.DialURL(authc.Rails.URL)
			if err != nil {
				errlog.Fatal().Err(err).Send()
			}

			pwd := authc.Rails.Password
			if len(pwd) != 0 {
				if _, err := c.Do("AUTH", pwd); err != nil {
					errlog.Fatal().Err(err).Send()
				}
			}
			return c, err
		},
	}

	return func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie(cookie)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		key := fmt.Sprintf("session:%s", ck.Value)
		sessionData, err := redis.Bytes(rp.Get().Do("GET", key))
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		userID, err := rails.ParseCookie(string(sessionData))
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func railsMemcacheHandler(authc configAuth, next http.Handler) http.HandlerFunc {
	cookie := authc.Cookie
	if len(cookie) == 0 {
		errlog.Fatal().Msg("no auth.cookie defined")
	}

	if len(authc.Rails.URL) == 0 {
		errlog.Fatal().Msg("no auth.rails.url defined")
	}

	rURL, err := url.Parse(authc.Rails.URL)
	if err != nil {
		errlog.Fatal().Err(err).Send()
	}

	mc := memcache.New(rURL.Host)

	return func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie(cookie)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		key := fmt.Sprintf("session:%s", ck.Value)
		item, err := mc.Get(key)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		userID, err := rails.ParseCookie(string(item.Value))
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func railsCookieHandler(authc configAuth, next http.Handler) http.HandlerFunc {
	cookie := authc.Cookie
	if len(cookie) == 0 {
		errlog.Fatal().Msg("no auth.cookie defined")
	}

	ra, err := railsAuth(authc)
	if err != nil {
		errlog.Fatal().Err(err).Send()
	}

	return func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie(cookie)
		if err != nil || len(ck.Value) == 0 {
			logger.Warn().Err(err).Msg("rails cookie missing")
			next.ServeHTTP(w, r)
			return
		}

		userID, err := ra.ParseCookie(ck.Value)
		if err != nil {
			logger.Warn().Err(err).Msg("failed to parse rails cookie")
			next.ServeHTTP(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func railsAuth(authc configAuth) (*rails.Auth, error) {
	secret := authc.Rails.SecretKeyBase
	if len(secret) == 0 {
		return nil, errors.New("no auth.rails.secret_key_base defined")
	}

	version := authc.Rails.Version
	if len(version) == 0 {
		return nil, errors.New("no auth.rails.version defined")
	}

	ra, err := rails.NewAuth(version, secret)
	if err != nil {
		return nil, err
	}

	if len(authc.Rails.Salt) != 0 {
		ra.Salt = authc.Rails.Salt
	}

	if len(authc.Rails.SignSalt) != 0 {
		ra.SignSalt = authc.Rails.SignSalt
	}

	if len(authc.Rails.AuthSalt) != 0 {
		ra.AuthSalt = authc.Rails.AuthSalt
	}

	return ra, nil
}
