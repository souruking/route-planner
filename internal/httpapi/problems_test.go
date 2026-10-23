package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const validProblemJSON = `{
  "name": "Almaty morning run",
  "depot": {"lat": 43.238, "lon": 76.945},
  "vehicles": [{"id": "van-1", "capacity": 40}],
  "stops": [
    {"id": "order-1", "lat": 43.256, "lon": 76.928, "demand": 5},
    {"id": "order-2", "lat": 43.221, "lon": 76.851, "demand": 12}
  ]
}`

func postProblem(t *testing.T, body, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	s := New(&fakePinger{}, newFakeStore(), discardLogger())
	r := httptest.NewRequest(http.MethodPost, "/problems", strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)
	return w
}

func TestCreateProblemErrors(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		contentType string
		wantStatus  int
		wantFields  []string // every field expected in the error, in order
	}{
		{
			name:        "wrong content type",
			body:        validProblemJSON,
			contentType: "text/plain",
			wantStatus:  http.StatusUnsupportedMediaType,
		},
		{
			name:        "unknown field",
			body:        `{"name":"x","demnd":5}`,
			contentType: "application/json",
			wantStatus:  http.StatusBadRequest,
			wantFields:  []string{"demnd"},
		},
		{
			name:        "missing depot",
			body:        `{"name":"x","vehicles":[{"id":"v","capacity":5}],"stops":[{"id":"s","lat":1,"lon":1,"demand":1}]}`,
			contentType: "application/json",
			wantStatus:  http.StatusUnprocessableEntity,
			wantFields:  []string{"depot"},
		},
		{
			name: "missing lat is not treated as 0",
			body: `{"name":"x","depot":{"lat":1,"lon":1},
			        "vehicles":[{"id":"v","capacity":5}],
			        "stops":[{"id":"s","lon":1,"demand":1}]}`,
			contentType: "application/json",
			wantStatus:  http.StatusUnprocessableEntity,
			wantFields:  []string{"stops[0].lat"},
		},
		{
			name: "missing fields and rule violations reported together",
			body: `{"name":"","depot":{"lon":1},
			        "vehicles":[{"id":"v","capacity":0}],
			        "stops":[{"id":"s","lat":1,"lon":1,"demand":1}]}`,
			contentType: "application/json",
			wantStatus:  http.StatusUnprocessableEntity,
			wantFields:  []string{"depot.lat", "name", "vehicles[0].capacity"},
		},
		{
			name: "infeasible problem",
			body: `{"name":"x","depot":{"lat":1,"lon":1},
			        "vehicles":[{"id":"v","capacity":5}],
			        "stops":[{"id":"s","lat":1,"lon":1,"demand":9}]}`,
			contentType: "application/json",
			wantStatus:  http.StatusUnprocessableEntity,
			wantFields:  []string{"stops[0].demand", "stops"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := postProblem(t, tt.body, tt.contentType)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", w.Code, tt.wantStatus, w.Body.String())
			}

			body := decodeErrorBody(t, w)
			if len(body.Fields) != len(tt.wantFields) {
				t.Fatalf("fields = %+v, want %v", body.Fields, tt.wantFields)
			}
			for i, want := range tt.wantFields {
				if body.Fields[i].Field != want {
					t.Errorf("fields[%d] = %q, want %q", i, body.Fields[i].Field, want)
				}
			}
		})
	}
}
