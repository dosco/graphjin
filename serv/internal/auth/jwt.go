package auth

import (
	"net/http"

	jwt "github.com/dgrijalva/jwt-go"

	"github.com/dosco/graphjin/serv/internal/auth/provider"
)

const (
	authHeader = "Authorization"
)

func JwtHandler(ac *Auth, next http.Handler) (http.HandlerFunc, error) {
	jwtProvider, err := provider.NewProvider(ac.JWT)
	if err != nil {
		return nil, err
	}

	cookie := ac.Cookie

	return func(w http.ResponseWriter, r *http.Request) {

		var tok string

		if cookie != "" {
			ck, err := r.Cookie(cookie)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			tok = ck.Value
		} else {
			ah := r.Header.Get(authHeader)
			if len(ah) < 10 {
				next.ServeHTTP(w, r)
				return
			}
			tok = ah[7:]
		}

		keyFunc := jwtProvider.KeyFunc()

		token, err := jwt.ParseWithClaims(tok, jwt.MapClaims{}, keyFunc) //jwt.MapClaims is already passed by reference

		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			ctx := r.Context()

			if !jwtProvider.VerifyAudience(claims) {
				next.ServeHTTP(w, r)
				return
			}

			if !jwtProvider.VerifyIssuer(claims) {
				next.ServeHTTP(w, r)
				return
			}

			ctx, err = jwtProvider.SetContextValues(ctx, claims)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		next.ServeHTTP(w, r)
	}, nil
}
