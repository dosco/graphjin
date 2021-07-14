// +build !magiclink

package auth

import (
	"database/sql"
	"errors"
	"net/http"
)

func MagicLinkHandler(ac *Auth, next http.Handler, db *sql.DB) (handlerFunc, error) {
	return nil, errors.New("rebuild with the 'magiclink' tag")
}
