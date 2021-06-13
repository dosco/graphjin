package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/dosco/graphjin/core"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/dosco/graphjin/serv/internal/auth/provider"

	"github.com/magiclabs/magic-admin-go/token"

	"github.com/magiclabs/magic-admin-go"
	"github.com/magiclabs/magic-admin-go/client"
)

type JWTConfig = provider.JWTConfig

// Auth struct contains authentication related config values used by the GraphJin service
type Auth struct {
	// Name is a friendly name for this auth config
	Name string
	// Type can be magiclink, rails, jwt or header
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

	// Magic.link authentication
	MagicLink struct {
		Secret string `mapstructure:"secret"`
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

const authBearer = "Bearer"

func MagicLinkHandler(ac *Auth, next http.Handler) (handlerFunc, error) {

	return func(w http.ResponseWriter, r *http.Request) (context.Context, error) {
		ctx := r.Context()
		// FIXME, all errors should be returned as valid responses
		// Currently will mix responses and say something like:
		// 'Malformed DID token error: illegal base64 data at input byte 120{"data":{"users":null}}'

		if !strings.HasPrefix(r.Header.Get("Authorization"), authBearer) {
			fmt.Fprintf(w, "Bearer token is required")
			return nil, fmt.Errorf("401 Bearer token is required")
		}

		did := r.Header.Get("Authorization")[len(authBearer)+1:]
		if did == "" {
			fmt.Fprintf(w, "DID token is required")
			return nil, fmt.Errorf("401 Bearer token not a DID token")
		}

		tk, err := token.NewToken(did)
		if err != nil {
			fmt.Fprintf(w, "Malformed DID token error: %s", err.Error())
			return nil, fmt.Errorf("Malformed DID token error: %s", err.Error())
		}

		if err := tk.Validate(); err != nil {
			fmt.Fprintf(w, "DID token failed validation: %s", err.Error())
			return nil, fmt.Errorf("DID token failed validation: %s", err.Error())
		}

		secret := ac.MagicLink.Secret
		if secret == "" {
			return nil, fmt.Errorf("Magic.link config secret is empty, we can't get metadata from user")
		}

		m := client.New(secret, magic.NewDefaultClient())
		userInfo, err := m.User.GetMetadataByIssuer(tk.GetIssuer())
		if err != nil {
			return nil, fmt.Errorf("Magic.link error: %s", err.Error())
		}

		if userInfo.Issuer != tk.GetIssuer() {
			return nil, fmt.Errorf("Unauthorized user login")
		}

		fmt.Printf("Lets seewhay er have : %#v\n", tk)
		ctx = context.WithValue(ctx, core.UserIDKey, userInfo.Email)

		next.ServeHTTP(w, r.WithContext(ctx))

		return ctx, nil
	}, nil
}

var err401 = errors.New("401 unauthorized")

func HeaderHandler(ac *Auth, next http.Handler) (handlerFunc, error) {
	hdr := ac.Header

	if hdr.Name == "" {
		return nil, fmt.Errorf("auth '%s': no header.name defined", ac.Name)
	}

	if !hdr.Exists && hdr.Value == "" {
		return nil, fmt.Errorf("auth '%s': no header.value defined", ac.Name)
	}

	return func(w http.ResponseWriter, r *http.Request) (context.Context, error) {
		var fo1 bool
		value := r.Header.Get(hdr.Name)

		switch {
		case hdr.Exists:
			fo1 = (value == "")

		default:
			fo1 = (value != hdr.Value)
		}

		if fo1 {
			return nil, err401
		}
		return nil, nil
	}, nil
}

type handlerFunc func(w http.ResponseWriter, r *http.Request) (context.Context, error)

func WithAuth(next http.Handler, ac *Auth, log *zap.Logger) (http.Handler, error) {
	var err error

	if ac.CredsInHeader {
		next, err = SimpleHandler(ac, next)
	}

	if err != nil {
		return nil, err
	}

	var h handlerFunc

	switch ac.Type {
	case "rails":
		h, err = RailsHandler(ac, next)

	case "jwt":
		h, err = JwtHandler(ac, next)

	case "header":
		h, err = HeaderHandler(ac, next)

	case "magiclink":
		h, err = MagicLinkHandler(ac, next)

	default:
		return next, nil
	}

	if err != nil {
		return nil, err
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, err := h(w, r)
		if err == err401 {
			http.Error(w, "401 unauthorized", http.StatusUnauthorized)
			return
		}

		if err != nil && log != nil {
			log.Error("Auth", []zapcore.Field{zap.String("type", ac.Type), zap.Error(err)}...)
		}

		if ctx != nil {
			next.ServeHTTP(w, r.WithContext(ctx))
		} else {
			next.ServeHTTP(w, r)
		}
	}), nil
}

func IsAuth(ct context.Context) bool {
	return ct.Value(core.UserIDKey) != nil
}
