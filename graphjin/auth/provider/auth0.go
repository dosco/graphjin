package provider

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"errors"
	"fmt"
	"strings"

	core "github.com/dosco/graphjin/core/v3"
	jwt "github.com/golang-jwt/jwt/v5"
)

type Auth0Provider struct {
	key    interface{}
	aud    string
	issuer string
}

// NewAuth0Provider creates a new Auth0 JWT provider
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

// KeyFunc returns a function that returns the key used to verify the JWT token
func (p *Auth0Provider) KeyFunc() jwt.Keyfunc {
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

// VerifyAudience checks if the audience claim is valid
func (p *Auth0Provider) VerifyAudience(claims jwt.MapClaims) bool {
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

// VerifyIssuer checks if the issuer claim is valid
func (p *Auth0Provider) VerifyIssuer(claims jwt.MapClaims) bool {
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
