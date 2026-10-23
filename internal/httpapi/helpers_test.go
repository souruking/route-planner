package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// bufferLogger returns a logger that writes JSON lines into buf,
// so tests can inspect what was logged.
func bufferLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, nil))
}

// fakePinger lets tests control whether the "database" is reachable.
type fakePinger struct {
	err   error
	calls int
}

func (f *fakePinger) Ping(ctx context.Context) error {
	f.calls++
	return f.err
}

func decodeJSONBody(resp *http.Response, dst any) error {
	return json.NewDecoder(resp.Body).Decode(dst)
}

// decodeErrorBody parses a standard error response or fails the test.
func decodeErrorBody(t *testing.T, w *httptest.ResponseRecorder) errorPayload {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not a JSON error body: %v\nbody: %s", err, w.Body.String())
	}
	return body.Error
}
