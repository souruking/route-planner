package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusCreated, map[string]string{"id": "abc"})

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got["id"] != "abc" {
		t.Fatalf("body = %s, err = %v", w.Body.String(), err)
	}
}

func TestWriteError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantFields int
		wantLogged bool // only 5xx should be logged at error level
	}{
		{
			name:       "not found",
			err:        errNotFound("problem"),
			wantStatus: http.StatusNotFound,
			wantCode:   "not_found",
		},
		{
			name: "validation with fields",
			err: errValidation([]FieldError{
				{Field: "name", Message: "must not be empty"},
				{Field: "stops[0].lat", Message: "must be between -90 and 90"},
			}),
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "validation_failed",
			wantFields: 2,
		},
		{
			name:       "wrapped *Error is still recognised",
			err:        fmt.Errorf("get problem: %w", errNotFound("problem")),
			wantStatus: http.StatusNotFound,
			wantCode:   "not_found",
		},
		{
			name:       "unknown error becomes 500",
			err:        errors.New("pq: password authentication failed for user app"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
			wantLogged: true,
		},
		{
			name:       "explicit 5xx *Error is logged",
			err:        errNotImplemented(),
			wantStatus: http.StatusNotImplemented,
			wantCode:   "not_implemented",
			wantLogged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			s := New(&fakePinger{}, newFakeStore(), bufferLogger(&buf))

			// Run through requestID so the body should carry an ID.
			h := s.requestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				s.writeError(w, r, tt.err)
			}))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			body := decodeErrorBody(t, w)
			if body.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", body.Code, tt.wantCode)
			}
			if len(body.Fields) != tt.wantFields {
				t.Errorf("fields = %+v, want %d", body.Fields, tt.wantFields)
			}
			if body.RequestID == "" || body.RequestID != w.Header().Get("X-Request-ID") {
				t.Errorf("request_id in body = %q, header = %q", body.RequestID, w.Header().Get("X-Request-ID"))
			}
			if logged := buf.Len() > 0; logged != tt.wantLogged {
				t.Errorf("logged = %v, want %v (log: %s)", logged, tt.wantLogged, buf.String())
			}
			if strings.Contains(w.Body.String(), "password") {
				t.Errorf("internal error leaked: %s", w.Body.String())
			}
		})
	}
}

func TestHandleAdapter(t *testing.T) {
	s := New(&fakePinger{}, newFakeStore(), discardLogger())

	t.Run("returned error is written", func(t *testing.T) {
		h := s.handle(func(w http.ResponseWriter, r *http.Request) error {
			return errNotFound("problem")
		})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", w.Code)
		}
	})

	t.Run("nil error leaves the response alone", func(t *testing.T) {
		h := s.handle(func(w http.ResponseWriter, r *http.Request) error {
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return nil
		})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ok":true`) {
			t.Fatalf("got %d %s", w.Code, w.Body.String())
		}
	})
}

func TestErrorUnwrap(t *testing.T) {
	cause := errors.New("connection reset")
	err := &Error{Status: 500, Code: "internal_error", Message: "x", Err: cause}

	if !errors.Is(err, cause) {
		t.Fatal("errors.Is should find the underlying cause")
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("Error() = %q, should include cause for logs", err.Error())
	}
}
