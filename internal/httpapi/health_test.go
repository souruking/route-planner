package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	db := &fakePinger{err: errors.New("db down")}
	s := New(db, discardLogger())

	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	// Liveness must not depend on the database.
	if db.calls != 0 {
		t.Fatalf("healthz pinged the database %d times, want 0", db.calls)
	}
}

func TestReadyz(t *testing.T) {
	tests := []struct {
		name       string
		pingErr    error
		wantStatus int
	}{
		{name: "database reachable", pingErr: nil, wantStatus: http.StatusOK},
		{name: "database down", pingErr: errors.New("connection refused"), wantStatus: http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &fakePinger{err: tt.pingErr}
			s := New(db, discardLogger())

			w := httptest.NewRecorder()
			s.Routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if db.calls != 1 {
				t.Fatalf("Ping called %d times, want 1", db.calls)
			}
		})
	}
}
