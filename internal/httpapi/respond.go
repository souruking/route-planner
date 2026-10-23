package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
)

// writeJSON encodes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	// Headers are already sent, so an encode error can't change the response.
	// It almost always means the client disconnected.
	_ = json.NewEncoder(w).Encode(v)
}

// errorBody is the JSON shape of every error response.
type errorBody struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code      string       `json:"code"`
	Message   string       `json:"message"`
	Fields    []FieldError `json:"fields,omitempty"`
	RequestID string       `json:"request_id,omitempty"`
}

// writeError is the single place where errors become HTTP responses.
// Known *Error values are sent as-is; anything else is logged and hidden behind a 500.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		apiErr = &Error{
			Status:  http.StatusInternalServerError,
			Code:    "internal_error",
			Message: "internal server error",
			Err:     err,
		}
	}

	if apiErr.Status >= 500 {
		s.log.ErrorContext(r.Context(), "request failed",
			"request_id", RequestIDFrom(r.Context()),
			"code", apiErr.Code,
			"err", err,
		)
	}

	writeJSON(w, apiErr.Status, errorBody{Error: errorPayload{
		Code:      apiErr.Code,
		Message:   apiErr.Message,
		Fields:    apiErr.Fields,
		RequestID: RequestIDFrom(r.Context()),
	}})
}

// handlerFunc is an http.HandlerFunc that can return an error.
type handlerFunc func(w http.ResponseWriter, r *http.Request) error

// handle adapts a handlerFunc to net/http, sending any returned error through writeError.
// Handlers then end with `return err` instead of repeating error-writing code.
func (s *Server) handle(h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			s.writeError(w, r, err)
		}
	}
}
