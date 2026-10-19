package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	jwt "github.com/golang-jwt/jwt/v5"

	"github.com/dosco/graphjin/auth/v3/provider"
	"github.com/dosco/graphjin/core/v3"
)

const (
	authHeader = "Authorization"
)

// JwtHandler is a middleware that checks for a JWT token in the cookie or the
// authorization header. If the token is found, it is validated and the claims
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
			if ah := r.Header.Get(authHeader); len(ah) > 7 && strings.HasPrefix(ah, "Bearer ") {
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

			return withVerifiedClaims(ctx, jwtProvider, claims)
		}
		return nil, fmt.Errorf("invalid claims")
	}, nil
}

// ContextWithClaims attaches claims to ctx as JwtHandler does after it
// verifies a token. It is for in-process callers that already trust the
// claims, such as a host that runs GraphJin as a library; it checks no
// signature, so it needs no verification key: without a usable JWT config
// the subject maps to the user ID as the generic provider maps it.
func ContextWithClaims(ctx context.Context, ac Auth, claims map[string]interface{}) (context.Context, error) {
	jwtProvider, err := provider.NewProvider(ac.JWT)
	if err != nil {
		jwtProvider = claimsOnlyProvider{}
	}
	return withVerifiedClaims(ctx, jwtProvider, jwt.MapClaims(claims))
}

// claimsOnlyProvider maps claims to context values and verifies nothing.
type claimsOnlyProvider struct{ *provider.GenericProvider }

func (claimsOnlyProvider) SetContextValues(ctx context.Context, claims jwt.MapClaims) (context.Context, error) {
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return ctx, fmt.Errorf("subject claim not found")
	}
	return context.WithValue(ctx, core.UserIDKey, sub), nil
}

func withVerifiedClaims(ctx context.Context, jwtProvider provider.JWTProvider, claims jwt.MapClaims) (context.Context, error) {
	ctx, err := jwtProvider.SetContextValues(ctx, claims)
	if err != nil {
		return ctx, err
	}

	// Attach optional identity claims for audit logging. These are
	// strictly informational — the role / authorization decision is
	// driven by sub / role.
	if email, ok := claims["email"].(string); ok && email != "" {
		ctx = context.WithValue(ctx, userEmailKey, email)
	}
	if name, ok := claims["name"].(string); ok && name != "" {
		ctx = context.WithValue(ctx, userNameKey, name)
	}
	ctx = context.WithValue(ctx, userClaimsKey, cloneJWTClaims(claims))
	return ctx, nil
}

func cloneJWTClaims(claims jwt.MapClaims) map[string]interface{} {
	out := make(map[string]interface{}, len(claims))
	for k, v := range claims {
		out[k] = v
	}
	return out
}
