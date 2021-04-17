package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/dosco/graphjin/core"

	"github.com/dosco/graphjin/serv/internal/auth/provider"
)

type JWTConfig = provider.JWTConfig

// Auth struct contains authentication related config values used by the GraphJin service
type Auth struct {
	// Name is a friendly name for this auth config
	Name string
	// Type can be rails, jwt or header
	Type string

	// Cookie is the name of the cookie used
	Cookie string

	// CredsInHeader is used in dev only to allow for credentials
	// in header values. Example: The X-User-ID HTTP header can be used to
	// set the user id.
	CredsInHeader bool `mapstructure:"creds_in_header"`

	// SubsCredsInVars is used in dev only to allow for credentials
	// in websocket variable. Example: The user_id websocket variable can be used to
	// set the user id.
	SubsCredsInVars bool `mapstructure:"subs_creds_in_vars"`

	// Rails cookie authentication
	Rails struct {
		// Rails version is needed to decode the cookie correctly.
		// Can be 5.2 or 6
		Version string

		// SecretKeyBase is the cookie encryption key used in your Rails config
		SecretKeyBase string `mapstructure:"secret_key_base"`

		// URL is used for Rails cookie store based auth.
		// Example: redis://redis-host:6379 or memcache://memcache-host
		URL string

		// Password is set if needed by Redis or Memcache
		Password string

		// MaxIdle maximum idle time for the connection
		MaxIdle int `mapstructure:"max_idle"`

		// MaxActive maximum active time for the connection
		MaxActive int `mapstructure:"max_active"`

		// Salt value is from your Rails 5.2 and below auth config
		Salt string

		// SignSalt value is from your Rails 5.2 and below auth config
		SignSalt string `mapstructure:"sign_salt"`

		// AuthSalt value is from your Rails 5.2 and below auth config
		AuthSalt string `mapstructure:"auth_salt"`
	}

	// JWT  authentication
	JWT JWTConfig

	// Header authentication
	Header struct {
		// Name of the HTTP header
		Name string

		// Value if set must match expected value (optional)
		Value string

		// Exists if set to true then the header must exist
		// this is an alternative to using value
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
