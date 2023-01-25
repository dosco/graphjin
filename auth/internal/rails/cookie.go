package rails

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1" // #nosec G505
	"encoding/base64"
	"net/url"
	"strings"

	"github.com/adjust/gorails/session"
	"golang.org/x/crypto/pbkdf2"
)

func parseCookie(cookie, secretKeyBase, salt, signSalt string) ([]byte, error) {
	return session.DecryptSignedCookie(
		cookie,
		secretKeyBase,
		salt,
		signSalt)
}

// {"session_id":"a71d6ffcd4ed5572ea2097f569eb95ef","warden.user.user.key":[[2],"$2a$11$q9Br7m4wJxQvF11hAHvTZO"],"_csrf_token":"HsYgrD2YBaWAabOYceN0hluNRnGuz49XiplmMPt43aY="}

func parseCookie52(cookie, secretKeyBase, authSalt string) ([]byte, error) {
	ecookie, err := url.QueryUnescape(cookie)
	if err != nil {
		return nil, err
	}

	vectors := strings.Split(ecookie, "--")

	body, err := base64.RawStdEncoding.DecodeString(vectors[0])
	if err != nil {
		return nil, err
	}

	iv, err := base64.RawStdEncoding.DecodeString(vectors[1])
	if err != nil {
		return nil, err
	}

	tag, err := base64.StdEncoding.DecodeString(vectors[2])
	if err != nil {
		return nil, err
	}

	key := pbkdf2.Key([]byte(secretKeyBase), []byte(authSalt),
		1000, 32, sha1.New)

	c, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(c)
	if err != nil {
		return nil, err
	}

	return gcm.Open(nil, iv, append(body, tag...), nil)
}
