package api

import "fmt"

type apiError struct {
	Status    int
	Code      string
	Message   string
	Details   any
	Retryable bool
}

func (e apiError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	if e.Message == "" {
		return e.Code
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func newAPIError(status int, code, message string) apiError {
	return apiError{Status: status, Code: code, Message: message}
}
