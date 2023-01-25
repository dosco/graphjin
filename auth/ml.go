//go:build magiclink
// +build magiclink

package auth

// TODO: Can't add this back in until the magiclabs library removes its
// btcd and eth dependencies.

/*
import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/dosco/graphjin/core/v3"
	jwt "github.com/golang-jwt/jwt"
	"github.com/magiclabs/magic-admin-go"
	"github.com/magiclabs/magic-admin-go/client"
	"github.com/magiclabs/magic-admin-go/token"
)

func MagicLinkHandler(ac *Auth, next http.Handler) (handlerFunc, error) {
	secret := ac.MagicLink.Secret
	if secret == "" {
		return nil, fmt.Errorf("magiclink config secret not set")
	}

	cookie := ac.Cookie
	if cookie == "" {
		return nil, fmt.Errorf("config cookie not set")
	}

	if ac.CookieExpiry == "" {
		ac.CookieExpiry = "2h"
	}

	expiryMinutes, err := time.ParseDuration(ac.CookieExpiry)
	if err != nil {
		return nil, fmt.Errorf("config cookie expiry: %s", err.Error())
	}

	signingKey := []byte(ac.MagicLink.Secret)

	kf := func(token *jwt.Token) (interface{}, error) {
		return signingKey, nil
	}

	return func(w http.ResponseWriter, r *http.Request) (context.Context, error) {
		ctx := r.Context()

		if ck, err := r.Cookie(cookie); err == nil {
			claims, err := validateJWT(ck.Value, "", "self", kf)
			if err != nil {
				return nil, err
			}

			ctx = context.WithValue(ctx, core.UserIDKey, claims["sub"])
			return ctx, nil
		}

		ah := r.Header.Get(authHeader)
		if len(ah) < 10 {
			return nil, fmt.Errorf("invalid or missing header: %s", authHeader)
		}

		did := ah[7:]
		if did == "" {
			return nil, fmt.Errorf("malformed bearer token")
		}

		tk, err := token.NewToken(did)
		if err != nil {
			return nil, fmt.Errorf("malformed bearer token: %s", err.Error())
		}

		if err := tk.Validate(); err != nil {
			return nil, fmt.Errorf("invalid bearer token: %s", err.Error())
		}

		m := client.New(secret, magic.NewDefaultClient())
		userInfo, err := m.User.GetMetadataByIssuer(tk.GetIssuer())
		if err != nil {
			return nil, err
		}

		if userInfo.Issuer != tk.GetIssuer() {
			return nil, fmt.Errorf("invalid issuer")
		}

		// Create the Claims
		claims := &jwt.StandardClaims{
			Issuer:    "self",
			ExpiresAt: time.Now().Add(expiryMinutes).Unix(),
			Subject:   userInfo.Email,
		}

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		signedJwtToken, err := token.SignedString(signingKey)
		if err != nil {
			return nil, err
		}

		webHost, err := url.Parse(r.Host)
		if err != nil {
			return nil, err
		}

		domain := webHost.Hostname()
		ck := http.Cookie{
			Name:     cookie,
			Value:    signedJwtToken,
			Expires:  time.Now().Add(expiryMinutes),
			HttpOnly: true,
			Secure:   ac.CookieHTTPS,
			Domain:   domain,
			Path:     "/",
		}
		ck.SameSite = http.SameSiteLaxMode
		http.SetCookie(w, &ck)

		ctx = context.WithValue(ctx, core.UserIDKey, userInfo.Email)
		return ctx, nil
	}, nil
}
*/
