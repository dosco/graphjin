package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dosco/graphjin/core/v3"
	jwt "github.com/golang-jwt/jwt"
)

const (
	firebasePKEndpoint   = "https://www.googleapis.com/robot/v1/metadata/x509/securetoken@system.gserviceaccount.com"
	firebaseIssuerPrefix = "https://securetoken.google.com/"
)

type firebasePKCache struct {
	PublicKeys map[string]string
	Expiration time.Time
	lock       sync.RWMutex
}

var firebasePublicKeys = firebasePKCache{
	lock: sync.RWMutex{},
}

type FirebaseProvider struct {
	aud    string
	issuer string
}

func NewFirebaseProvider(config JWTConfig) (*FirebaseProvider, error) {
	issuer := config.Issuer
	if issuer == "" {
		issuer = firebaseIssuerPrefix + config.Audience
	}
	return &FirebaseProvider{
		aud:    config.Audience,
		issuer: issuer,
	}, nil
}

func (p *FirebaseProvider) KeyFunc() jwt.Keyfunc {
	return firebaseKeyFunction
}

func (p *FirebaseProvider) VerifyAudience(claims jwt.MapClaims) bool {
	if claims == nil {
		return false
	}
	return claims.VerifyAudience(p.aud, p.aud != "")
}

func (p *FirebaseProvider) VerifyIssuer(claims jwt.MapClaims) bool {
	if claims == nil {
		return false
	}
	return claims.VerifyIssuer(p.issuer, p.issuer != "")
}

func (p *FirebaseProvider) SetContextValues(ctx context.Context, claims jwt.MapClaims) (context.Context, error) {
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

type firebaseKeyError struct {
	Err     error
	Message string
}

func (e *firebaseKeyError) Error() string {
	return e.Message + " " + e.Err.Error()
}

func firebaseKeyFunction(token *jwt.Token) (interface{}, error) {
	kid, ok := token.Header["kid"]

	if !ok {
		return nil, &firebaseKeyError{
			Message: "Error 'kid' header not found in token",
		}
	}

	firebasePublicKeys.lock.RLock()
	pastExpiration := firebasePublicKeys.Expiration.Before(time.Now())
	firebasePublicKeys.lock.RUnlock()
	if pastExpiration {
		resp, err := http.Get(firebasePKEndpoint)
		if err != nil {
			return nil, &firebaseKeyError{
				Message: "Error connecting to firebase certificate server",
				Err:     err,
			}
		}

		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, &firebaseKeyError{
				Message: "Error reading firebase certificate server response",
				Err:     err,
			}
		}

		cachePolicy := resp.Header.Get("cache-control")
		ageIndex := strings.Index(cachePolicy, "max-age=")

		if ageIndex < 0 {
			return nil, &firebaseKeyError{
				Message: "Error parsing cache-control header: 'max-age=' not found",
			}
		}

		ageToEnd := cachePolicy[ageIndex+8:]
		endIndex := strings.Index(ageToEnd, ",")
		if endIndex < 0 {
			endIndex = len(ageToEnd) - 1
		}
		ageString := ageToEnd[:endIndex]

		age, err := strconv.ParseInt(ageString, 10, 64)
		if err != nil {
			return nil, &firebaseKeyError{
				Message: "Error parsing max-age cache policy",
				Err:     err,
			}
		}

		expiration := time.Now().Add(time.Duration(time.Duration(age) * time.Second))

		firebasePublicKeys.lock.Lock()
		err = json.Unmarshal(data, &firebasePublicKeys.PublicKeys)

		if err != nil {
			firebasePublicKeys.lock.Unlock()
			return nil, &firebaseKeyError{
				Message: "Error unmarshalling firebase public key json",
				Err:     err,
			}
		}

		firebasePublicKeys.Expiration = expiration
		firebasePublicKeys.lock.Unlock()
	}

	firebasePublicKeys.lock.RLock()
	defer firebasePublicKeys.lock.RUnlock()
	if key, found := firebasePublicKeys.PublicKeys[kid.(string)]; found {
		k, err := jwt.ParseRSAPublicKeyFromPEM([]byte(key))
		return k, err
	}

	return nil, &firebaseKeyError{
		Message: "Error no matching public key for kid supplied in jwt",
	}
}
