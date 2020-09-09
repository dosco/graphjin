package auth

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/dosco/super-graph/core"
)

const (
	authHeader               = "Authorization"
	jwtAuth0             int = iota + 1
	jwtFirebase          int = iota + 2
	firebasePKEndpoint       = "https://www.googleapis.com/robot/v1/metadata/x509/securetoken@system.gserviceaccount.com"
	firebaseIssuerPrefix     = "https://securetoken.google.com/"
)

type firebasePKCache struct {
	PublicKeys map[string]string
	Expiration time.Time
	lock       sync.RWMutex
}

var firebasePublicKeys = firebasePKCache{
	lock: sync.RWMutex{},
}

func JwtHandler(ac *Auth, next http.Handler) (http.HandlerFunc, error) {
	var key interface{}
	var jwtProvider int

	cookie := ac.Cookie

	if ac.JWT.Provider == "auth0" {
		jwtProvider = jwtAuth0
	} else if ac.JWT.Provider == "firebase" {
		jwtProvider = jwtFirebase
	}

	secret := ac.JWT.Secret
	publicKeyFile := ac.JWT.PubKeyFile

	switch {
	case secret != "":
		key = []byte(secret)

	case publicKeyFile != "":
		kd, err := ioutil.ReadFile(publicKeyFile)
		if err != nil {
			return nil, err
		}

		switch ac.JWT.PubKeyType {
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

	return func(w http.ResponseWriter, r *http.Request) {

		var tok string

		if cookie != "" {
			ck, err := r.Cookie(cookie)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			tok = ck.Value
		} else {
			ah := r.Header.Get(authHeader)
			if len(ah) < 10 {
				next.ServeHTTP(w, r)
				return
			}
			tok = ah[7:]
		}

		var keyFunc jwt.Keyfunc
		if jwtProvider == jwtFirebase {
			keyFunc = firebaseKeyFunction
		} else {
			keyFunc = func(token *jwt.Token) (interface{}, error) {
				return key, nil
			}
		}

		token, err := jwt.ParseWithClaims(tok, &jwt.StandardClaims{}, keyFunc)

		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		if claims, ok := token.Claims.(*jwt.StandardClaims); ok {
			ctx := r.Context()

			if ac.JWT.Audience != "" && claims.Audience != ac.JWT.Audience {
				next.ServeHTTP(w, r)
				return
			}

			if jwtProvider == jwtAuth0 {
				sub := strings.Split(claims.Subject, "|")
				if len(sub) != 2 {
					ctx = context.WithValue(ctx, core.UserIDProviderKey, sub[0])
					ctx = context.WithValue(ctx, core.UserIDKey, sub[1])
				}
			} else if jwtProvider == jwtFirebase &&
				claims.Issuer == firebaseIssuerPrefix+ac.JWT.Audience {
				ctx = context.WithValue(ctx, core.UserIDKey, claims.Subject)
			} else {
				ctx = context.WithValue(ctx, core.UserIDKey, claims.Subject)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		next.ServeHTTP(w, r)
	}, nil
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

		data, err := ioutil.ReadAll(resp.Body)

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
