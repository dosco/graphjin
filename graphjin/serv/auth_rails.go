package serv

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/dosco/super-graph/rails"
	"github.com/garyburd/redigo/redis"
)

func railsRedisHandler(next http.Handler) http.HandlerFunc {
	cookie := conf.Auth.Cookie
	if len(cookie) == 0 {
		errlog.Fatal().Msg("no auth.cookie defined")
	}

	if len(conf.Auth.Rails.URL) == 0 {
		errlog.Fatal().Msg("no auth.rails.url defined")
	}

	rp := &redis.Pool{
		MaxIdle:   conf.Auth.Rails.MaxIdle,
		MaxActive: conf.Auth.Rails.MaxActive,
		Dial: func() (redis.Conn, error) {
			c, err := redis.DialURL(conf.Auth.Rails.URL)
			if err != nil {
				errlog.Fatal().Err(err).Send()
			}

			pwd := conf.Auth.Rails.Password
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

func railsMemcacheHandler(next http.Handler) http.HandlerFunc {
	cookie := conf.Auth.Cookie
	if len(cookie) == 0 {
		errlog.Fatal().Msg("no auth.cookie defined")
	}

	if len(conf.Auth.Rails.URL) == 0 {
		errlog.Fatal().Msg("no auth.rails.url defined")
	}

	rURL, err := url.Parse(conf.Auth.Rails.URL)
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

func railsCookieHandler(next http.Handler) http.HandlerFunc {
	cookie := conf.Auth.Cookie
	if len(cookie) == 0 {
		errlog.Fatal().Msg("no auth.cookie defined")
	}

	ra, err := railsAuth(conf)
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

func railsAuth(c *config) (*rails.Auth, error) {
	secret := c.Auth.Rails.SecretKeyBase
	if len(secret) == 0 {
		return nil, errors.New("no auth.rails.secret_key_base defined")
	}

	version := c.Auth.Rails.Version
	if len(version) == 0 {
		return nil, errors.New("no auth.rails.version defined")
	}

	ra, err := rails.NewAuth(version, secret)
	if err != nil {
		return nil, err
	}

	if len(c.Auth.Rails.Salt) != 0 {
		ra.Salt = c.Auth.Rails.Salt
	}

	if len(conf.Auth.Rails.SignSalt) != 0 {
		ra.SignSalt = c.Auth.Rails.SignSalt
	}

	if len(conf.Auth.Rails.AuthSalt) != 0 {
		ra.AuthSalt = c.Auth.Rails.AuthSalt
	}

	return ra, nil
}
