// +build magiclink

package auth

import (
	"fmt"

	jwt "github.com/golang-jwt/jwt"
)

func validateJWT(tok, aud, iss string, keyFunc jwt.Keyfunc) (jwt.MapClaims, error) {
	token, err := jwt.ParseWithClaims(tok, jwt.MapClaims{}, keyFunc) //jwt.MapClaims is already passed by reference

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		if !claims.VerifyAudience(aud, aud != "") {
			return nil, fmt.Errorf("invalid aud claim")
		}

		if !claims.VerifyIssuer(iss, iss != "") {
			return nil, fmt.Errorf("invalid iss claim")
		}

		if err := claims.Valid(); err != nil {
			return nil, err
		}

		return claims, err
	}
	return nil, fmt.Errorf("invalid claims")
}
