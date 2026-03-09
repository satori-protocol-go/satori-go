package client

import "fmt"

type RequestError struct {
	StatusCode int
	Body       string
}

func (e *RequestError) Error() string {
	if e == nil {
		return ""
	}
	if e.Body != "" {
		return fmt.Sprintf("request failed with status %d: %s", e.StatusCode, e.Body)
	}
	return fmt.Sprintf("request failed with status %d", e.StatusCode)
}

func IsStatus(err error, statusCode int) bool {
	if err == nil {
		return false
	}
	requestErr, ok := err.(*RequestError)
	if !ok {
		return false
	}
	return requestErr.StatusCode == statusCode
}
