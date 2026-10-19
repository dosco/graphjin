package provider

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"errors"
	"fmt"

	"github.com/dosco/graphjin/core/v3"
	jwt "github.com/golang-jwt/jwt/v5"
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
		switch p.key.(type) {
		case []byte:
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
		case *rsa.PublicKey:
			if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
		case *ecdsa.PublicKey:
			if _, ok := token.Method.(*jwt.SigningMethodECDSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
		default:
			return nil, fmt.Errorf("unsupported key type")
		}
		return p.key, nil
	}
}

// VerifyAudience verifies the audience claim of the JWT token
func (p *GenericProvider) VerifyAudience(claims jwt.MapClaims) bool {
	if claims == nil {
		return false
	}
	if p.aud == "" {
		return true
	}
	aud, err := claims.GetAudience()
	if err != nil {
		return false
	}
	for _, a := range aud {
		if a == p.aud {
			return true
		}
	}
	return false
}

// VerifyIssuer verifies the issuer claim of the JWT token
func (p *GenericProvider) VerifyIssuer(claims jwt.MapClaims) bool {
	if claims == nil {
		return false
	}
	if p.issuer == "" {
		return true
	}
	iss, err := claims.GetIssuer()
	if err != nil {
		return false
	}
	return iss == p.issuer
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
