package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRoutes checks the wiring of Routes(): methods, paths, and the middleware chain.
func TestRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantJSON   bool // standard JSON error body expected
	}{
		{"healthz", http.MethodGet, "/healthz", http.StatusOK, false},
		{"readyz", http.MethodGet, "/readyz", http.StatusOK, false},
		{"create problem stub", http.MethodPost, "/problems", http.StatusNotImplemented, true},
		{"list problems stub", http.MethodGet, "/problems", http.StatusNotImplemented, true},
		{"get problem stub", http.MethodGet, "/problems/123", http.StatusNotImplemented, true},
		{"delete problem stub", http.MethodDelete, "/problems/123", http.StatusNotImplemented, true},
		{"unknown path", http.MethodGet, "/nope", http.StatusNotFound, false},
		{"wrong method", http.MethodPut, "/problems", http.StatusMethodNotAllowed, false},
	}

	s := New(&fakePinger{}, discardLogger())
	handler := s.Routes()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(tt.method, tt.path, nil))

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", w.Code, tt.wantStatus, w.Body.String())
			}
			// requestID middleware wraps every route, including 404/405.
			if w.Header().Get("X-Request-ID") == "" {
				t.Error("X-Request-ID header missing")
			}
			if tt.wantJSON {
				body := decodeErrorBody(t, w)
				if body.Code == "" {
					t.Error("error code missing")
				}
			}
		})
	}
}

func TestMethodNotAllowedListsAllowedMethods(t *testing.T) {
	s := New(&fakePinger{}, discardLogger())
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/problems", nil))

	allow := w.Header().Get("Allow")
	for _, m := range []string{"GET", "POST"} {
		if !strings.Contains(allow, m) {
			t.Errorf("Allow = %q, missing %s", allow, m)
		}
	}
}
