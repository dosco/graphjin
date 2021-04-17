package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/dosco/graphjin/core"

	"github.com/dosco/graphjin/serv/internal/auth/provider"
)

// Auth struct contains authentication related config values used by the GraphJin service
type Auth struct {
	Name            string
	Type            string
	Cookie          string
	CredsInHeader   bool `mapstructure:"creds_in_header"`
	SubsCredsInVars bool `mapstructure:"subs_creds_in_vars"`

	Rails struct {
		Version       string
		SecretKeyBase string `mapstructure:"secret_key_base"`
		URL           string
		Password      string
		MaxIdle       int `mapstructure:"max_idle"`
		MaxActive     int `mapstructure:"max_active"`
		Salt          string
		SignSalt      string `mapstructure:"sign_salt"`
		AuthSalt      string `mapstructure:"auth_salt"`
	}

	JWT provider.JWTConfig

	Header struct {
		Name   string
		Value  string
		Exists bool
	}
}

func SimpleHandler(ac *Auth, next http.Handler) (http.HandlerFunc, error) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		userIDProvider := r.Header.Get("X-User-ID-Provider")
		if userIDProvider != "" {
			ctx = context.WithValue(ctx, core.UserIDProviderKey, userIDProvider)
		}

		userID := r.Header.Get("X-User-ID")
		if userID != "" {
			ctx = context.WithValue(ctx, core.UserIDKey, userID)
		}

		userRole := r.Header.Get("X-User-Role")
		if userRole != "" {
			ctx = context.WithValue(ctx, core.UserRoleKey, userRole)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	}, nil
}

func HeaderHandler(ac *Auth, next http.Handler) (http.HandlerFunc, error) {
	hdr := ac.Header

	if hdr.Name == "" {
		return nil, fmt.Errorf("auth '%s': no header.name defined", ac.Name)
	}

	if !hdr.Exists && hdr.Value == "" {
		return nil, fmt.Errorf("auth '%s': no header.value defined", ac.Name)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		var fo1 bool
		value := r.Header.Get(hdr.Name)

		switch {
		case hdr.Exists:
			fo1 = (value == "")

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

func WithAuth(next http.Handler, ac *Auth) (http.Handler, error) {
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
