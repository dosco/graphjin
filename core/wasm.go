//go:build wasm

package core

import (
	"errors"
)

func (gj *graphjin) introspection(query string) ([]byte, error) {
	return nil, errors.New("introspection not supported")
}
