package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/dosco/graphjin/auth/v3/internal/rails"
	"github.com/dosco/graphjin/core/v3"
	"github.com/gomodule/redigo/redis"
)

func RailsHandler(ac Auth) (HandlerFunc, error) {
	ru := ac.Rails.URL

	if strings.HasPrefix(ru, "memcache:") {
		return RailsMemcacheHandler(ac)
	}

	if strings.HasPrefix(ru, "redis:") {
		return RailsRedisHandler(ac)
	}

	return RailsCookieHandler(ac)
}

func RailsRedisHandler(ac Auth) (HandlerFunc, error) {
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
			if pwd != "" {
				if _, err := c.Do("AUTH", pwd); err != nil {
					return nil, err
				}
			}

			return c, nil
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			if time.Since(t) < (time.Second * 30) {
				return nil
			}
			_, err := c.Do("PING")
			return err
		},
	}

	return func(w http.ResponseWriter, r *http.Request) (context.Context, error) {
		ck, err := r.Cookie(cookie)
		if err == http.ErrNoCookie {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}

		re := rp.Get()
		defer re.Close()

		key := fmt.Sprintf("session:%s", ck.Value)
		sessionData, err := redis.Bytes(re.Do("GET", key))
		if err != nil {
			return nil, err
		}

		userID, err := rails.ParseCookie(string(sessionData))
		if err != nil {
			return nil, err
		}

		ctx := context.WithValue(r.Context(), core.UserIDKey, userID)
		return ctx, nil
	}, nil
}

func RailsMemcacheHandler(ac Auth) (HandlerFunc, error) {
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

	return func(w http.ResponseWriter, r *http.Request) (context.Context, error) {
		ck, err := r.Cookie(cookie)
		if err == http.ErrNoCookie {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}

		key := fmt.Sprintf("session:%s", ck.Value)
		item, err := mc.Get(key)
		if err != nil {
			return nil, err
		}

		userID, err := rails.ParseCookie(string(item.Value))
		if err != nil {
			return nil, err
		}

		ctx := context.WithValue(r.Context(), core.UserIDKey, userID)
		return ctx, nil
	}, nil
}

func RailsCookieHandler(ac Auth) (HandlerFunc, error) {
	cookie := ac.Cookie
	if len(cookie) == 0 {
		return nil, fmt.Errorf("no auth.cookie defined")
	}

	ra, err := railsAuth(ac)
	if err != nil {
		return nil, err
	}

	return func(w http.ResponseWriter, r *http.Request) (context.Context, error) {
		ck, err := r.Cookie(cookie)
		if err == http.ErrNoCookie {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}

		userID, err := ra.ParseCookie(ck.Value)
		if err != nil {
			return nil, err
		}

		ctx := context.WithValue(r.Context(), core.UserIDKey, userID)
		return ctx, nil
	}, nil
}

func railsAuth(ac Auth) (*rails.Auth, error) {
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

	if ac.Rails.Salt != "" {
		ra.Salt = ac.Rails.Salt
	}

	if ac.Rails.SignSalt != "" {
		ra.SignSalt = ac.Rails.SignSalt
	}

	if ac.Rails.AuthSalt != "" {
		ra.AuthSalt = ac.Rails.AuthSalt
	}

	return ra, nil
}
