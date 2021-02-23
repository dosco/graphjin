package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/lestrrat-go/jwx/jwk"

	"github.com/dosco/graphjin/core"
)

type keychainCache struct {
	jwksURL  string
	keyCache *jwk.AutoRefresh //local in-memory cache to store keys
}

func newKeychainCache(jwksURL string, refreshInterval, minRefreshInterval int) *keychainCache {
	ar := jwk.NewAutoRefresh(context.Background())
	if refreshInterval > 0 {
		ar.Configure(jwksURL, jwk.WithRefreshInterval(time.Duration(refreshInterval)*time.Minute))
	} else if minRefreshInterval > 0 {
		ar.Configure(jwksURL, jwk.WithMinRefreshInterval(time.Duration(minRefreshInterval)*time.Minute))
	} else {
		ar.Configure(jwksURL)
	}
	return &keychainCache{
		jwksURL:  jwksURL,
		keyCache: ar,
	}
}

func (k *keychainCache) getKey(kid string) (interface{}, error) {
	set, err := k.keyCache.Fetch(context.TODO(), k.jwksURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	if key, found := set.LookupKeyID(kid); found {
		var rawkey interface{}
		err := key.Raw(&rawkey)
		if err != nil {
			return nil, fmt.Errorf("failed to create key: %w", err)
		}
		return rawkey, nil
	}
	return nil, errors.New("key not found")
}

type JWKSProvider struct {
	aud    string
	issuer string
	cache  *keychainCache
}

func NewJWKSProvider(config JWTConfig) (*JWKSProvider, error) {
	if config.JWKSURL == "" {
		return nil, errors.New("undefined JWKSURL")
	}
	return &JWKSProvider{
		aud:    config.Audience,
		issuer: config.Issuer,
		cache:  newKeychainCache(config.JWKSURL, config.JWKSRefresh, config.JWKSMinRefresh),
	}, nil
}

func (p *JWKSProvider) KeyFunc() jwt.Keyfunc {
	return func(token *jwt.Token) (interface{}, error) {
		if token == nil {
			return nil, errors.New("null token")
		}
		if token.Header == nil {
			return nil, errors.New("null token header")
		}
		kid, found := token.Header["kid"].(string)
		if !found {
			return nil, errors.New("kid not found")
		}
		key, err := p.cache.getKey(kid)
		if err != nil {
			return nil, err
		}
		if key == nil {
			return nil, errors.New("key not found")
		}
		return key, nil
	}
}

func (p *JWKSProvider) VerifyAudience(claims jwt.MapClaims) bool {
	if claims == nil {
		return false
	}
	return claims.VerifyAudience(p.aud, p.aud != "")
}

func (p *JWKSProvider) VerifyIssuer(claims jwt.MapClaims) bool {
	if claims == nil {
		return false
	}
	return claims.VerifyIssuer(p.issuer, p.issuer != "")
}

func (p *JWKSProvider) SetContextValues(ctx context.Context, claims jwt.MapClaims) (context.Context, error) {
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
