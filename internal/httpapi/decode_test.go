package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testPayload struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestDecodeJSON(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		contentType string
		wantStatus  int // 0 means success
		wantCode    string
		wantField   string
	}{
		{name: "valid", body: `{"name":"a","count":1}`, contentType: "application/json"},
		{name: "valid with charset", body: `{"name":"a"}`, contentType: "application/json; charset=utf-8"},
		{name: "empty body", body: ``, wantStatus: 400, wantCode: "invalid_json"},
		{name: "malformed", body: `{"name":`, wantStatus: 400, wantCode: "invalid_json"},
		{name: "syntax error", body: `{"name" "a"}`, wantStatus: 400, wantCode: "invalid_json"},
		{name: "wrong type", body: `{"count":"x"}`, wantStatus: 400, wantCode: "invalid_json", wantField: "count"},
		{name: "unknown field", body: `{"nmae":"a"}`, wantStatus: 400, wantCode: "invalid_json", wantField: "nmae"},
		{name: "trailing data", body: `{"name":"a"}{}`, wantStatus: 400, wantCode: "invalid_json"},
		{name: "array instead of object", body: `[1,2]`, wantStatus: 400, wantCode: "invalid_json"},
		{name: "wrong content type", body: `{}`, contentType: "text/plain", wantStatus: 415, wantCode: "unsupported_media_type"},
		{name: "too large", body: `{"name":"` + strings.Repeat("a", int(maxBodyBytes)) + `"}`, wantStatus: 413, wantCode: "payload_too_large"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			if tt.contentType != "" {
				r.Header.Set("Content-Type", tt.contentType)
			}
			w := httptest.NewRecorder()

			var dst testPayload
			err := decodeJSON(w, r, &dst)

			if tt.wantStatus == 0 {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			var apiErr *Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("want *Error, got %T: %v", err, err)
			}
			if apiErr.Status != tt.wantStatus || apiErr.Code != tt.wantCode {
				t.Fatalf("got %d %s, want %d %s", apiErr.Status, apiErr.Code, tt.wantStatus, tt.wantCode)
			}
			if tt.wantField != "" && (len(apiErr.Fields) == 0 || apiErr.Fields[0].Field != tt.wantField) {
				t.Fatalf("want field %q, got %+v", tt.wantField, apiErr.Fields)
			}
		})
	}
}

func TestWriteErrorHidesInternalErrors(t *testing.T) {
	s := New(nil, discardLogger())
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	s.writeError(w, r, errors.New("pq: password authentication failed"))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "password") {
		t.Fatalf("internal error leaked to client: %s", w.Body.String())
	}

	var body errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if body.Error.Code != "internal_error" {
		t.Fatalf("code = %q, want internal_error", body.Error.Code)
	}
}
