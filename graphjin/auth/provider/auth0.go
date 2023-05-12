package provider

import (
	"context"
	"errors"
	"strings"

	core "github.com/dosco/graphjin/core/v3"
	jwt "github.com/golang-jwt/jwt"
)

type Auth0Provider struct {
	key    interface{}
	aud    string
	issuer string
}

func NewAuth0Provider(config JWTConfig) (*Auth0Provider, error) {
	key, err := getKey(config)
	if err != nil {
		return nil, err
	}
	return &Auth0Provider{
		key:    key,
		aud:    config.Audience,
		issuer: config.Issuer,
	}, nil
}

func (p *Auth0Provider) KeyFunc() jwt.Keyfunc {
	return func(token *jwt.Token) (interface{}, error) {
		return p.key, nil
	}
}

func (p *Auth0Provider) VerifyAudience(claims jwt.MapClaims) bool {
	if claims == nil {
		return false
	}
	return claims.VerifyAudience(p.aud, p.aud != "")
}

func (p *Auth0Provider) VerifyIssuer(claims jwt.MapClaims) bool {
	if claims == nil {
		return false
	}
	return claims.VerifyIssuer(p.issuer, p.issuer != "")
}

func (p *Auth0Provider) SetContextValues(ctx context.Context, claims jwt.MapClaims) (context.Context, error) {
	if claims == nil {
		return ctx, errors.New("undefined claims")
	}
	sub, found := claims["sub"].(string)
	if !found || sub == "" {
		return ctx, errors.New("sub claim not found")
	}
	sp := strings.SplitN(sub, "|", 2)
	if len(sp) == 2 {
		ctx = context.WithValue(ctx, core.UserIDRawKey, sub)
		ctx = context.WithValue(ctx, core.UserIDProviderKey, sp[0])
		ctx = context.WithValue(ctx, core.UserIDKey, sp[1])
	} else {
		ctx = context.WithValue(ctx, core.UserIDKey, sub)
	}
	return ctx, nil
}
