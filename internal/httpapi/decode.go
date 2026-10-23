package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

// maxBodyBytes caps request bodies. 1 MB comfortably fits 1000 stops.
const maxBodyBytes int64 = 1 << 20

// decodeJSON reads exactly one JSON object from the request body into dst.
// On failure it returns an *Error with a client-friendly message.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	// 1. Content-Type must be JSON (parameters like charset are allowed).
	if ct := r.Header.Get("Content-Type"); ct != "" {
		mediaType, _, err := mime.ParseMediaType(ct)
		if err != nil || mediaType != "application/json" {
			return errUnsupportedMediaType()
		}
	}

	// 2. Limit body size; reading past the limit returns *http.MaxBytesError.
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	// 3. Reject fields the struct doesn't know about (catches typos like "demnd").
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return translateDecodeError(err)
	}

	// 4. The body must contain a single JSON value, nothing after it.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errInvalidJSON("request body must contain a single JSON object")
	}

	return nil
}

// translateDecodeError turns encoding/json errors into readable API errors.
func translateDecodeError(err error) error {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	var maxBytesErr *http.MaxBytesError

	switch {
	case errors.As(err, &syntaxErr):
		return errInvalidJSON(fmt.Sprintf("malformed JSON at byte %d", syntaxErr.Offset))

	// Decoder sometimes reports truncated JSON as ErrUnexpectedEOF instead of a SyntaxError.
	case errors.Is(err, io.ErrUnexpectedEOF):
		return errInvalidJSON("malformed JSON")

	case errors.As(err, &typeErr):
		field := typeErr.Field
		if field == "" {
			field = "body"
		}
		return errInvalidJSON("wrong type for field", FieldError{
			Field:   field,
			Message: fmt.Sprintf("must be of type %s", typeErr.Type),
		})

	case errors.Is(err, io.EOF):
		return errInvalidJSON("request body must not be empty")

	// encoding/json has no typed error for unknown fields, only this message.
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		name := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
		return errInvalidJSON("unknown field", FieldError{
			Field:   name,
			Message: "is not allowed",
		})

	case errors.As(err, &maxBytesErr):
		return errPayloadTooLarge(maxBytesErr.Limit)

	default:
		// Anything else (e.g. connection reset mid-body) is unexpected: becomes a 500.
		return fmt.Errorf("decode request body: %w", err)
	}
}
