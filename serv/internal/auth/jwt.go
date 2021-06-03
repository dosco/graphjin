package auth

import (
	"context"
	"fmt"
	"net/http"

	jwt "github.com/dgrijalva/jwt-go"

	"github.com/dosco/graphjin/serv/internal/auth/provider"
)

const (
	authHeader = "Authorization"
)

func JwtHandler(ac *Auth, next http.Handler) (handlerFunc, error) {
	jwtProvider, err := provider.NewProvider(ac.JWT)
	if err != nil {
		return nil, err
	}

	cookie := ac.Cookie

	return func(w http.ResponseWriter, r *http.Request) (context.Context, error) {
		var tok string

		if cookie != "" {
			ck, err := r.Cookie(cookie)
			if err != nil {
				return nil, err
			}
			tok = ck.Value
		} else {
			ah := r.Header.Get(authHeader)
			if len(ah) < 10 {
				return nil, fmt.Errorf("invalid jwt in header: %s", authHeader)
			}
			tok = ah[7:]
		}

		if tok == "" {
			return nil, fmt.Errorf("jwt not found")
		}

		keyFunc := jwtProvider.KeyFunc()

		token, err := jwt.ParseWithClaims(tok, jwt.MapClaims{}, keyFunc) //jwt.MapClaims is already passed by reference

		if err != nil {
			return nil, err
		}

		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			ctx := r.Context()

			if !jwtProvider.VerifyAudience(claims) {
				return nil, fmt.Errorf("invalid aud claim")
			}

			if !jwtProvider.VerifyIssuer(claims) {
				return nil, fmt.Errorf("invalid iss claim")
			}

			ctx, err = jwtProvider.SetContextValues(ctx, claims)
			return ctx, err
		}
		return nil, fmt.Errorf("invalid claims")
	}, nil
}
