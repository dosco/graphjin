package provider

import (
	"context"
	"errors"
	"io/ioutil"

	jwt "github.com/dgrijalva/jwt-go"
)

// JWTConfig struct contains JWT authentication related config values used by
// the GraphJin service
type JWTConfig struct {
	Provider       string
	Secret         string
	PubKeyFile     string `mapstructure:"public_key_file"`
	PubKeyType     string `mapstructure:"public_key_type"`
	Audience       string `mapstructure:"audience"`
	Issuer         string `mapstructure:"issuer"`           //like "http://my-domain.auth0.com"
	JWKSURL        string `mapstructure:"jwks_url"`         //URL of the JWKS endpoint, like "https://YOUR_DOMAIN/.well-known/jwks.json"
	JWKSRefresh    int    `mapstructure:"jwks_refresh"`     //static minutes interval between refreshes, overriding the adaptive token refreshing
	JWKSMinRefresh int    `mapstructure:"jwks_min_refresh"` //minutes fallback value when tokens are refreshed, default to 60 minutes
}

// JWTProvider is the interface to define providers for doing JWT
// authentication.
type JWTProvider interface {
	KeyFunc() jwt.Keyfunc
	VerifyAudience(jwt.MapClaims) bool
	VerifyIssuer(jwt.MapClaims) bool
	SetContextValues(context.Context, jwt.MapClaims) (context.Context, error)
}

func NewProvider(config JWTConfig) (JWTProvider, error) {
	switch config.Provider {
	case "auth0":
		return NewAuth0Provider(config)
	case "firebase":
		return NewFirebaseProvider(config)
	case "jwks":
		return NewJWKSProvider(config)
	default:
		return NewGenericProvider(config)
	}
}

func getKey(config JWTConfig) (interface{}, error) {
	var key interface{}
	secret := config.Secret
	publicKeyFile := config.PubKeyFile
	switch {
	case secret != "":
		key = []byte(secret)
	case publicKeyFile != "":
		kd, err := ioutil.ReadFile(publicKeyFile)
		if err != nil {
			return nil, err
		}
		switch config.PubKeyType {
		case "ecdsa":
			key, err = jwt.ParseECPublicKeyFromPEM(kd)
		case "rsa":
			key, err = jwt.ParseRSAPublicKeyFromPEM(kd)
		default:
			key, err = jwt.ParseECPublicKeyFromPEM(kd)
		}
		if err != nil {
			return nil, err
		}
	}
	if key == nil {
		return nil, errors.New("undefined key")
	}
	return key, nil
}
