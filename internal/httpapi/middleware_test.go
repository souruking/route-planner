package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestID(t *testing.T) {
	s := New(&fakePinger{}, newFakeStore(), discardLogger())

	// Captures the ID the handler sees in its context.
	var seen string
	h := s.requestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
	}))

	t.Run("generates an ID when none is sent", func(t *testing.T) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

		got := w.Header().Get("X-Request-ID")
		if len(got) != 16 { // 8 random bytes, hex-encoded
			t.Fatalf("generated ID = %q, want 16 hex chars", got)
		}
		if seen != got {
			t.Fatalf("context ID %q != header ID %q", seen, got)
		}
	})

	t.Run("generated IDs are unique", func(t *testing.T) {
		ids := map[string]bool{}
		for range 100 {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
			ids[w.Header().Get("X-Request-ID")] = true
		}
		if len(ids) != 100 {
			t.Fatalf("got %d unique IDs out of 100", len(ids))
		}
	})

	t.Run("reuses an incoming ID", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-Request-ID", "client-abc-123")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		if got := w.Header().Get("X-Request-ID"); got != "client-abc-123" {
			t.Fatalf("header ID = %q, want client-abc-123", got)
		}
		if seen != "client-abc-123" {
			t.Fatalf("context ID = %q, want client-abc-123", seen)
		}
	})

	t.Run("replaces an oversized incoming ID", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-Request-ID", strings.Repeat("x", 65))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		if got := w.Header().Get("X-Request-ID"); len(got) != 16 {
			t.Fatalf("oversized ID was not replaced: %q", got)
		}
	})
}

func TestLogRequests(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantStatus int
		wantBytes  int
	}{
		{
			name: "explicit status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte("hello"))
			},
			wantStatus: http.StatusCreated,
			wantBytes:  5,
		},
		{
			name: "implicit 200 from Write",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("hi"))
			},
			wantStatus: http.StatusOK,
			wantBytes:  2,
		},
		{
			name:       "handler writes nothing",
			handler:    func(w http.ResponseWriter, r *http.Request) {},
			wantStatus: http.StatusOK,
			wantBytes:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			s := New(&fakePinger{}, newFakeStore(), bufferLogger(&buf))

			h := s.requestID(s.logRequests(tt.handler))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/problems", nil))

			var entry map[string]any
			if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
				t.Fatalf("log is not a single JSON line: %v\n%s", err, buf.String())
			}

			// JSON numbers decode as float64.
			if got := int(entry["status"].(float64)); got != tt.wantStatus {
				t.Errorf("logged status = %d, want %d", got, tt.wantStatus)
			}
			if got := int(entry["bytes"].(float64)); got != tt.wantBytes {
				t.Errorf("logged bytes = %d, want %d", got, tt.wantBytes)
			}
			if entry["method"] != "POST" || entry["path"] != "/problems" {
				t.Errorf("logged method/path = %v %v", entry["method"], entry["path"])
			}
			if entry["request_id"] != w.Header().Get("X-Request-ID") {
				t.Errorf("logged request_id %v != response header %q", entry["request_id"], w.Header().Get("X-Request-ID"))
			}
			if _, ok := entry["duration_ms"]; !ok {
				t.Error("duration_ms missing from log")
			}
		})
	}
}

func TestRecoverPanics(t *testing.T) {
	t.Run("panic becomes 500 and is logged", func(t *testing.T) {
		var buf bytes.Buffer
		s := New(&fakePinger{}, newFakeStore(), bufferLogger(&buf))

		h := s.recoverPanics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("boom")
		}))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", w.Code)
		}
		if strings.Contains(w.Body.String(), "boom") {
			t.Fatalf("panic value leaked to client: %s", w.Body.String())
		}
		if !strings.Contains(buf.String(), "boom") || !strings.Contains(buf.String(), "stack") {
			t.Fatalf("panic not logged with stack: %s", buf.String())
		}
	})

	t.Run("http.ErrAbortHandler is re-panicked", func(t *testing.T) {
		s := New(&fakePinger{}, newFakeStore(), discardLogger())
		h := s.recoverPanics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic(http.ErrAbortHandler)
		}))

		defer func() {
			if v := recover(); v != http.ErrAbortHandler {
				t.Fatalf("recovered %v, want http.ErrAbortHandler", v)
			}
		}()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		t.Fatal("expected panic to propagate")
	})

	t.Run("no panic passes through untouched", func(t *testing.T) {
		s := New(&fakePinger{}, newFakeStore(), discardLogger())
		h := s.recoverPanics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		}))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

		if w.Code != http.StatusTeapot {
			t.Fatalf("status = %d, want 418", w.Code)
		}
	})
}
