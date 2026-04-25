// Package issuer mints local JWTs signed with GraphJin's own secret / private
// key. Tokens it produces are verifiable by auth/provider.NewGenericProvider
// using the same JWTConfig, so the existing auth middleware needs no changes.
package issuer

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"errors"
	"fmt"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

// Config for the issuer. Mirrors the fields already on provider.JWTConfig that
// are relevant to signing; we keep it as its own type so callers can also pass
// in a PrivKey (not present on the verification-only JWTConfig).
type Config struct {
	// Secret for HS256 symmetric signing. Used if PrivKey is empty.
	Secret string

	// PrivKey is a PEM-encoded RSA or EC private key. If set, takes precedence
	// over Secret and we sign with RS256 / ES256 accordingly.
	PrivKey string

	// PrivKeyType selects the key type when parsing PrivKey. One of "rsa" or
	// "ecdsa". Defaults to "ecdsa" when empty.
	PrivKeyType string

	// Issuer and Audience claims applied to every token.
	Issuer   string
	Audience string

	// TTL applied to new tokens. Zero means the caller supplies `exp` directly.
	TTL time.Duration
}

// Issuer mints signed JWTs.
type Issuer struct {
	method jwt.SigningMethod
	key    interface{}
	iss    string
	aud    string
	ttl    time.Duration
}

// New returns an Issuer ready to mint tokens.
func New(cfg Config) (*Issuer, error) {
	i := &Issuer{iss: cfg.Issuer, aud: cfg.Audience, ttl: cfg.TTL}

	switch {
	case cfg.PrivKey != "":
		kt := cfg.PrivKeyType
		if kt == "" {
			kt = "ecdsa"
		}
		switch kt {
		case "rsa":
			k, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(cfg.PrivKey))
			if err != nil {
				return nil, fmt.Errorf("parse RSA private key: %w", err)
			}
			i.key = k
			i.method = jwt.SigningMethodRS256
		case "ecdsa":
			k, err := jwt.ParseECPrivateKeyFromPEM([]byte(cfg.PrivKey))
			if err != nil {
				return nil, fmt.Errorf("parse EC private key: %w", err)
			}
			i.key = k
			i.method = jwt.SigningMethodES256
		default:
			return nil, fmt.Errorf("unsupported PrivKeyType %q", cfg.PrivKeyType)
		}

	case cfg.Secret != "":
		i.key = []byte(cfg.Secret)
		i.method = jwt.SigningMethodHS256

	default:
		return nil, errors.New("issuer: neither Secret nor PrivKey set")
	}

	return i, nil
}

// Mint signs the given claims. If `exp` / `iat` / `iss` / `aud` are not set,
// they are filled in from the Issuer config. Returns the signed token string.
func (i *Issuer) Mint(claims jwt.MapClaims) (string, error) {
	if claims == nil {
		claims = jwt.MapClaims{}
	}
	now := time.Now()
	if _, ok := claims["iat"]; !ok {
		claims["iat"] = now.Unix()
	}
	if _, ok := claims["exp"]; !ok && i.ttl > 0 {
		claims["exp"] = now.Add(i.ttl).Unix()
	}
	if _, ok := claims["iss"]; !ok && i.iss != "" {
		claims["iss"] = i.iss
	}
	if _, ok := claims["aud"]; !ok && i.aud != "" {
		claims["aud"] = i.aud
	}
	tok := jwt.NewWithClaims(i.method, claims)
	return tok.SignedString(i.key)
}

// PublicKey returns the counterpart public key (for RSA/ECDSA issuers) or the
// shared secret bytes (for HS256). Useful for tests and admin/debug tooling.
func (i *Issuer) PublicKey() interface{} {
	switch k := i.key.(type) {
	case *rsa.PrivateKey:
		return &k.PublicKey
	case *ecdsa.PrivateKey:
		return &k.PublicKey
	default:
		return i.key
	}
}
