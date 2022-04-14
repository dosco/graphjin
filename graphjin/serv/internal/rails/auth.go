package rails

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/adjust/gorails/marshal"
)

const (
	salt          = "encrypted cookie"
	signSalt      = "signed encrypted cookie"
	authSalt      = "authenticated encrypted cookie"
	railsCipher   = "aes-256-cbc"
	railsCipher52 = "aes-256-gcm"
)

var (
	errSessionData = errors.New("error decoding session data")
)

type Auth struct {
	Cipher   string
	Secret   string
	Salt     string
	SignSalt string
	AuthSalt string
}

func NewAuth(version, secret string) (*Auth, error) {
	ra := &Auth{
		Secret:   secret,
		Salt:     salt,
		SignSalt: signSalt,
		AuthSalt: authSalt,
	}

	var v1, v2 int
	var err error

	sv := strings.Split(version, ".")
	if len(sv) >= 2 {
		if v1, err = strconv.Atoi(sv[0]); err != nil {
			return nil, err
		}
		if v2, err = strconv.Atoi(sv[1]); err != nil {
			return nil, err
		}
	}

	if v1 >= 5 && v2 >= 2 {
		ra.Cipher = railsCipher52
	} else {
		ra.Cipher = railsCipher
	}

	return ra, nil
}

func (ra Auth) ParseCookie(cookie string) (userID string, err error) {
	var dcookie []byte

	switch ra.Cipher {
	case railsCipher:
		dcookie, err = parseCookie(cookie, ra.Secret, ra.Salt, ra.SignSalt)

	case railsCipher52:
		dcookie, err = parseCookie52(cookie, ra.Secret, ra.AuthSalt)

	default:
		err = fmt.Errorf("unknown rails cookie cipher '%s'", ra.Cipher)
	}

	if err != nil {
		return
	}

	if dcookie[0] != '{' {
		userID, err = getUserId4(dcookie)
	} else {
		userID, err = getUserId(dcookie)
	}

	return
}

func ParseCookie(cookie string) (string, error) {
	if cookie[0] != '{' {
		return getUserId4([]byte(cookie))
	}

	return getUserId([]byte(cookie))
}

func getUserId(data []byte) (userID string, err error) {
	var sessionData map[string]interface{}

	err = json.Unmarshal(data, &sessionData)
	if err != nil {
		return
	}

	userKey, ok := sessionData["warden.user.user.key"]
	if !ok {
		err = errors.New("key 'warden.user.user.key' not found in session data")
	}

	items, ok := userKey.([]interface{})
	if !ok {
		err = errSessionData
		return
	}

	if len(items) != 2 {
		err = errSessionData
		return
	}

	uids, ok := items[0].([]interface{})
	if !ok {
		err = errSessionData
		return
	}

	uid, ok := uids[0].(float64)
	if !ok {
		err = errSessionData
		return
	}
	userID = fmt.Sprintf("%d", int64(uid))

	return
}

func getUserId4(data []byte) (userID string, err error) {
	sessionData, err := marshal.CreateMarshalledObject(data).GetAsMap()
	if err != nil {
		return
	}

	wardenData, ok := sessionData["warden.user.user.key"]
	if !ok {
		err = errSessionData
		return
	}

	wardenUserKey, err := wardenData.GetAsArray()
	if err != nil {
		return
	}
	if len(wardenUserKey) < 1 {
		err = errSessionData
		return
	}

	userData, err := wardenUserKey[0].GetAsArray()
	if err != nil {
		return
	}
	if len(userData) < 1 {
		err = errSessionData
		return
	}

	uid, err := userData[0].GetAsInteger()
	if err != nil {
		return
	}
	userID = fmt.Sprintf("%d", uid)

	return
}
