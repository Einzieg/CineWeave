package edition

import "fmt"

type AuthorizationError struct {
	Code      DenialCode
	Message   string
	Retryable bool
	Details   map[string]string
}

func (e AuthorizationError) Error() string {
	switch {
	case e.Code == "":
		return e.Message
	case e.Message == "":
		return string(e.Code)
	default:
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
}

func newAuthorizationError(code DenialCode, message string) AuthorizationError {
	return AuthorizationError{Code: code, Message: message}
}
