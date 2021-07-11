// +build magiclink

package auth

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/dosco/graphjin/core"
	"github.com/magiclabs/magic-admin-go"
	"github.com/magiclabs/magic-admin-go/client"
	"github.com/magiclabs/magic-admin-go/token"
)

func MagicLinkHandler(ac *Auth, next http.Handler, db *sql.DB) (handlerFunc, error) {
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

		if ac.MagicLink.AutoUpsertUser != "" {
			// 1. replace $user_id with $1
			//if !strings.Contains(ac.MagicLink.AutoUpsertUser, "$user_id") {
			//	return nil, fmt.Errorf("auto_upsert_user: $user_id variable missing")
			//}

			// 2, get a database connection (or better, reuse one we have since the DB has limited connections)
			conn, err := db.Conn(ctx)
			if err != nil {
				return nil, err
			}
			defer conn.Close()

			// 3. execute the  INSERT INTO...IN CONFLICT DO NOTHING
			_, err = conn.ExecContext(ctx, ac.MagicLink.AutoUpsertUser, userInfo.Email)
			if err != nil {
				return nil, err
			}

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
		if os.Getenv("GO_ENV") != "development" {
			ck.SameSite = http.SameSiteLaxMode
		}
		http.SetCookie(w, &ck)

		ctx = context.WithValue(ctx, core.UserIDKey, userInfo.Email)
		return ctx, nil
	}, nil
}
