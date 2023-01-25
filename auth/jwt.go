package auth

import (
	"context"
	"fmt"
	"net/http"

	jwt "github.com/golang-jwt/jwt"

	"github.com/dosco/graphjin/auth/v3/provider"
)

const (
	authHeader = "Authorization"
)

func JwtHandler(ac Auth) (HandlerFunc, error) {
	jwtProvider, err := provider.NewProvider(ac.JWT)
	if err != nil {
		return nil, err
	}

	cookie := ac.Cookie

	return func(w http.ResponseWriter, r *http.Request) (context.Context, error) {
		var tok string

		if cookie != "" {
			if ck, err := r.Cookie(cookie); err == nil && len(ck.Value) != 0 {
				tok = ck.Value
			}
		}

		if tok == "" {
			if ah := r.Header.Get(authHeader); len(ah) > 10 {
				tok = ah[7:]
			}
		}

		if tok == "" {
			return nil, fmt.Errorf("no jwt token found in cookie or authorization header")
		}

		keyFunc := jwtProvider.KeyFunc()
		token, err := jwt.ParseWithClaims(tok, jwt.MapClaims{}, keyFunc) // jwt.MapClaims is already passed by reference
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
