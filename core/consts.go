package core

import (
	"context"
	"errors"
)

const (
	openVar  = "{{"
	closeVar = "}}"
)

var (
	errNotFound = errors.New("not found in prepared statements")
)

func keyExists(ct context.Context, key contextkey) bool {
	return ct.Value(key) != nil
}
