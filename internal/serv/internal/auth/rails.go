package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/dosco/super-graph/core"
	"github.com/dosco/super-graph/internal/serv/internal/rails"
	"github.com/garyburd/redigo/redis"
)

func RailsHandler(ac *Auth, next http.Handler) (http.HandlerFunc, error) {
	ru := ac.Rails.URL

	if strings.HasPrefix(ru, "memcache:") {
		return RailsMemcacheHandler(ac, next)
	}

	if strings.HasPrefix(ru, "redis:") {
		return RailsRedisHandler(ac, next)
	}

	return RailsCookieHandler(ac, next)
}

func RailsRedisHandler(ac *Auth, next http.Handler) (http.HandlerFunc, error) {
	cookie := ac.Cookie

	if len(cookie) == 0 {
		return nil, fmt.Errorf("no auth.cookie defined")
	}

	if len(ac.Rails.URL) == 0 {
		return nil, fmt.Errorf("no auth.rails.url defined")
	}

	rp := &redis.Pool{
		MaxIdle:   ac.Rails.MaxIdle,
		MaxActive: ac.Rails.MaxActive,
		Dial: func() (redis.Conn, error) {
			c, err := redis.DialURL(ac.Rails.URL)
			if err != nil {
				return nil, err
			}

			pwd := ac.Rails.Password
			if len(pwd) != 0 {
				if _, err := c.Do("AUTH", pwd); err != nil {
					return nil, err
				}
			}

			return c, nil
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

		ctx := context.WithValue(r.Context(), core.UserIDKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}, nil
}

func RailsMemcacheHandler(ac *Auth, next http.Handler) (http.HandlerFunc, error) {
	cookie := ac.Cookie

	if len(cookie) == 0 {
		return nil, fmt.Errorf("no auth.cookie defined")
	}

	if len(ac.Rails.URL) == 0 {
		return nil, fmt.Errorf("no auth.rails.url defined")
	}

	rURL, err := url.Parse(ac.Rails.URL)
	if err != nil {
		return nil, err
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

		ctx := context.WithValue(r.Context(), core.UserIDKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}, nil
}

func RailsCookieHandler(ac *Auth, next http.Handler) (http.HandlerFunc, error) {
	cookie := ac.Cookie
	if len(cookie) == 0 {
		return nil, fmt.Errorf("no auth.cookie defined")
	}

	ra, err := railsAuth(ac)
	if err != nil {
		return nil, err
	}

	return func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie(cookie)
		if err != nil || len(ck.Value) == 0 {
			// logger.Warn().Err(err).Msg("rails cookie missing")
			next.ServeHTTP(w, r)
			return
		}

		userID, err := ra.ParseCookie(ck.Value)
		if err != nil {
			// logger.Warn().Err(err).Msg("failed to parse rails cookie")
			next.ServeHTTP(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), core.UserIDKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}, nil
}

func railsAuth(ac *Auth) (*rails.Auth, error) {
	secret := ac.Rails.SecretKeyBase
	if len(secret) == 0 {
		return nil, errors.New("no auth.rails.secret_key_base defined")
	}

	version := ac.Rails.Version
	if version == "" {
		return nil, errors.New("no auth.rails.version defined")
	}

	ra, err := rails.NewAuth(version, secret)
	if err != nil {
		return nil, err
	}

	if len(ac.Rails.Salt) != 0 {
		ra.Salt = ac.Rails.Salt
	}

	if len(ac.Rails.SignSalt) != 0 {
		ra.SignSalt = ac.Rails.SignSalt
	}

	if len(ac.Rails.AuthSalt) != 0 {
		ra.AuthSalt = ac.Rails.AuthSalt
	}

	return ra, nil
}
