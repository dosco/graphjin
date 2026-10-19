package core

import "errors"

// CodedError is an error with a stable machine-readable code. When it reaches
// a GraphQL response, the code is in the error's extensions.code, so callers
// can match the code instead of the message text.
type CodedError struct {
	Code    string
	Message string
}

func (e *CodedError) Error() string { return e.Message }

// NewCodedError returns an error that carries code.
func NewCodedError(code, message string) error {
	return &CodedError{Code: code, Message: message}
}

// ErrorCode returns the code of the first CodedError in err's chain, or "".
func ErrorCode(err error) string {
	var coded *CodedError
	if errors.As(err, &coded) {
		return coded.Code
	}
	return ""
}
