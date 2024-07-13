package provider

import (
	"context"
	"errors"

	"github.com/dosco/graphjin/core/v3"
	jwt "github.com/golang-jwt/jwt"
)

type GenericProvider struct {
	key    interface{}
	aud    string
	issuer string
}

// NewGenericProvider creates a new generic JWT provider
func NewGenericProvider(config JWTConfig) (*GenericProvider, error) {
	key, err := getKey(config)
	if err != nil {
		return nil, err
	}
	return &GenericProvider{
		key:    key,
		aud:    config.Audience,
		issuer: config.Issuer,
	}, nil
}

// KeyFunc returns a function that returns the key used to verify the JWT token
func (p *GenericProvider) KeyFunc() jwt.Keyfunc {
	return func(token *jwt.Token) (interface{}, error) {
		return p.key, nil
	}
}

// VerifyAudience verifies the audience claim of the JWT token
func (p *GenericProvider) VerifyAudience(claims jwt.MapClaims) bool {
	if claims == nil {
		return false
	}
	return claims.VerifyAudience(p.aud, p.aud != "")
}

// VerifyIssuer verifies the issuer claim of the JWT token
func (p *GenericProvider) VerifyIssuer(claims jwt.MapClaims) bool {
	if claims == nil {
		return false
	}
	return claims.VerifyIssuer(p.issuer, p.issuer != "")
}

// SetContextValues sets the user ID and provider in the context
func (p *GenericProvider) SetContextValues(ctx context.Context, claims jwt.MapClaims) (context.Context, error) {
	if claims == nil {
		return ctx, errors.New("undefined claims")
	}
	sub, found := claims["sub"].(string)
	if !found {
		return ctx, errors.New("subject claim not found")
	}
	ctx = context.WithValue(ctx, core.UserIDKey, sub)
	return ctx, nil
}
