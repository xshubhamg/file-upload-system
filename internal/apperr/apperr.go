// Package apperr defines the single error model used by services and mapped to
// HTTP responses by the transport layer. Services never import gin.
package apperr

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// Error is an application error with a stable machine-readable code.
// Message is safe to return to clients; Cause is kept for logs only.
type Error struct {
	Status  int
	Code    string
	Message string
	Details map[string]string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

// WithDetail attaches a field-level detail and returns the receiver.
func (e *Error) WithDetail(field, message string) *Error {
	if e.Details == nil {
		e.Details = make(map[string]string, 1)
	}
	e.Details[field] = message
	return e
}

// WithDetails merges several field-level details.
func (e *Error) WithDetails(details map[string]string) *Error {
	for field, message := range details {
		e.WithDetail(field, message)
	}
	return e
}

func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func Validation(message string) *Error {
	return New(http.StatusBadRequest, "VALIDATION_ERROR", message)
}

func Unauthorized(message string) *Error {
	return New(http.StatusUnauthorized, "UNAUTHORIZED", message)
}

func Forbidden(message string) *Error {
	return New(http.StatusForbidden, "FORBIDDEN", message)
}

func NotFound(resource string) *Error {
	return New(http.StatusNotFound, "NOT_FOUND", resource+" not found")
}

func Conflict(code, message string) *Error {
	return New(http.StatusConflict, code, message)
}

func PayloadTooLarge(message string) *Error {
	return New(http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", message)
}

func UnsupportedMedia(message string) *Error {
	return New(http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", message)
}

func TooManyRequests(message string) *Error {
	return New(http.StatusTooManyRequests, "TOO_MANY_REQUESTS", message)
}

// Internal wraps an unexpected error. The cause is logged but never exposed.
func Internal(err error) *Error {
	return &Error{
		Status:  http.StatusInternalServerError,
		Code:    "INTERNAL_ERROR",
		Message: "an unexpected error occurred",
		Cause:   err,
	}
}

// From normalises any error into *Error. Unknown errors become internal errors
// so callers cannot leak implementation details by accident.
func From(err error) *Error {
	if err == nil {
		return nil
	}

	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{
			Status:  http.StatusGatewayTimeout,
			Code:    "REQUEST_TIMEOUT",
			Message: "the request took too long",
			Cause:   err,
		}
	}

	return Internal(err)
}
