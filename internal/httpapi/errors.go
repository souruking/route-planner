package httpapi

import (
	"fmt"
	"net/http"
)

// FieldError points at one invalid field in the request body,
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Error is an error that knows how it should look to the client.
// Handlers return it; writeError turns it into a JSON response.
type Error struct {
	Status  int          // HTTP status code
	Code    string       // stable, machine-readable code, e.g. "validation_failed"
	Message string       // human-readable summary, safe to show to clients
	Fields  []FieldError // optional per-field details
	Err     error        // underlying cause: logged, never sent to the client
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return e.Code + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.Err }

// Constructors keep status codes and error codes consistent across handlers.

func errInvalidJSON(message string, fields ...FieldError) *Error {
	return &Error{
		Status:  http.StatusBadRequest,
		Code:    "invalid_json",
		Message: message,
		Fields:  fields,
	}
}

func errValidation(fields []FieldError) *Error {
	return &Error{
		Status:  http.StatusUnprocessableEntity,
		Code:    "validation_failed",
		Message: "request is invalid",
		Fields:  fields,
	}
}

func errNotFound(resource string) *Error {
	return &Error{
		Status:  http.StatusNotFound,
		Code:    "not_found",
		Message: resource + " not found",
	}
}

func errPayloadTooLarge(limit int64) *Error {
	return &Error{
		Status:  http.StatusRequestEntityTooLarge,
		Code:    "payload_too_large",
		Message: fmt.Sprintf("request body must not exceed %d bytes", limit),
	}
}

func errUnsupportedMediaType() *Error {
	return &Error{
		Status:  http.StatusUnsupportedMediaType,
		Code:    "unsupported_media_type",
		Message: "Content-Type must be application/json",
	}
}

func errNotImplemented() *Error {
	return &Error{
		Status:  http.StatusNotImplemented,
		Code:    "not_implemented",
		Message: "endpoint is not implemented yet",
	}
}
