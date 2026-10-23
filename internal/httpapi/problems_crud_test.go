package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/souruking/route-planner/internal/store"
)

// testAPI bundles a server, its fake store and a request helper.
type testAPI struct {
	t     *testing.T
	store *fakeStore
	h     http.Handler
}

func newTestAPI(t *testing.T) *testAPI {
	fs := newFakeStore()
	return &testAPI{t: t, store: fs, h: New(&fakePinger{}, fs, discardLogger()).Routes()}
}

func (a *testAPI) do(method, path, body string) *httptest.ResponseRecorder {
	a.t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	a.h.ServeHTTP(w, r)
	return w
}

// create posts validProblemJSON (with a given name) and returns the response body.
func (a *testAPI) create(name string) problemResponse {
	a.t.Helper()
	body := strings.Replace(validProblemJSON, `"Almaty morning run"`, `"`+name+`"`, 1)
	w := a.do(http.MethodPost, "/problems", body)
	if w.Code != http.StatusCreated {
		a.t.Fatalf("create: status %d, body %s", w.Code, w.Body.String())
	}
	var p problemResponse
	decodeInto(a.t, w, &p)
	return p
}

func decodeInto(t *testing.T, w *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, w.Body.String())
	}
}

// ---- create ----

func TestCreateProblem(t *testing.T) {
	api := newTestAPI(t)
	w := api.do(http.MethodPost, "/problems", validProblemJSON)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", w.Code, w.Body.String())
	}

	var p problemResponse
	decodeInto(t, w, &p)

	if got, want := w.Header().Get("Location"), "/problems/"+p.ID.String(); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
	if p.Name != "Almaty morning run" || p.Depot.Lat != 43.238 || p.Depot.Lon != 76.945 {
		t.Errorf("unexpected problem fields: %+v", p)
	}
	if len(p.Stops) != 2 || p.Stops[0].ID != "order-1" || p.Stops[1].Demand != 12 {
		t.Errorf("stops = %+v", p.Stops)
	}
	if len(p.Vehicles) != 1 || p.Vehicles[0].ID != "van-1" || p.Vehicles[0].Capacity != 40 {
		t.Errorf("vehicles = %+v", p.Vehicles)
	}
	if p.CreatedAt.IsZero() || p.CreatedAt.Location() != time.UTC {
		t.Errorf("created_at = %v, want a UTC time", p.CreatedAt)
	}
}

func TestCreateProblemTrimsName(t *testing.T) {
	api := newTestAPI(t)
	p := api.create("  padded  ")
	if p.Name != "padded" {
		t.Fatalf("name = %q, want %q", p.Name, "padded")
	}
}

func TestCreateProblemDoesNotStoreInvalidInput(t *testing.T) {
	api := newTestAPI(t)
	w := api.do(http.MethodPost, "/problems", `{"name":"","depot":{"lat":1,"lon":1},"vehicles":[],"stops":[]}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", w.Code)
	}
	if n := len(api.store.problems); n != 0 {
		t.Fatalf("store has %d problems, want 0", n)
	}
}

func TestCreateProblemStoreFailure(t *testing.T) {
	api := newTestAPI(t)
	api.store.err = errors.New("pq: connection refused to 10.0.0.5")

	w := api.do(http.MethodPost, "/problems", validProblemJSON)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "10.0.0.5") {
		t.Fatalf("internal error leaked: %s", w.Body.String())
	}
}

// ---- get ----

func TestGetProblem(t *testing.T) {
	api := newTestAPI(t)
	created := api.create("first")

	w := api.do(http.MethodGet, "/problems/"+created.ID.String(), "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var got problemResponse
	decodeInto(t, w, &got)
	if got.ID != created.ID || got.Name != "first" || len(got.Stops) != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestGetProblemNotFound(t *testing.T) {
	tests := []struct{ name, id string }{
		{"unknown uuid", uuid.NewString()},
		{"malformed id", "not-a-uuid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newTestAPI(t)
			w := api.do(http.MethodGet, "/problems/"+tt.id, "")

			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", w.Code)
			}
			if body := decodeErrorBody(t, w); body.Message != "problem not found" {
				t.Fatalf("message = %q", body.Message)
			}
		})
	}
}

// ---- list ----

func TestListProblemsEmpty(t *testing.T) {
	api := newTestAPI(t)
	w := api.do(http.MethodGet, "/problems", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	// Clients should get an empty array and an explicit null, never a missing key.
	want := `{"items":[],"next_cursor":null}`
	if got := strings.TrimSpace(w.Body.String()); got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
	if api.store.lastLimit != defaultListLimit {
		t.Fatalf("limit passed to store = %d, want default %d", api.store.lastLimit, defaultListLimit)
	}
}

func TestListProblemsPagination(t *testing.T) {
	api := newTestAPI(t)
	for _, name := range []string{"p1", "p2", "p3", "p4", "p5"} {
		api.create(name)
	}

	var names []string
	path := "/problems?limit=2"
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("pagination does not terminate")
		}

		w := api.do(http.MethodGet, path, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d (body: %s)", w.Code, w.Body.String())
		}
		var resp listProblemsResponse
		decodeInto(t, w, &resp)

		for _, it := range resp.Items {
			names = append(names, it.Name)
		}
		if resp.NextCursor == nil {
			break
		}
		path = "/problems?limit=2&cursor=" + *resp.NextCursor
	}

	if got := strings.Join(names, ","); got != "p5,p4,p3,p2,p1" {
		t.Fatalf("names across pages = %s, want newest first", got)
	}
}

func TestListProblemsInvalidParams(t *testing.T) {
	tests := []struct{ name, query, field string }{
		{"limit not a number", "limit=abc", "limit"},
		{"limit zero", "limit=0", "limit"},
		{"limit negative", "limit=-1", "limit"},
		{"limit too high", "limit=101", "limit"},
		{"cursor not base64", "cursor=***", "cursor"},
		{"cursor not json", "cursor=aGVsbG8", "cursor"},   // "hello"
		{"cursor missing fields", "cursor=e30", "cursor"}, // "{}"
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newTestAPI(t)
			w := api.do(http.MethodGet, "/problems?"+tt.query, "")

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", w.Code)
			}
			body := decodeErrorBody(t, w)
			if body.Code != "invalid_parameter" || len(body.Fields) != 1 || body.Fields[0].Field != tt.field {
				t.Fatalf("got %+v, want invalid_parameter on %q", body, tt.field)
			}
		})
	}
}

func TestListProblemsLimitBoundaries(t *testing.T) {
	for _, limit := range []string{"1", "100"} {
		api := newTestAPI(t)
		if w := api.do(http.MethodGet, "/problems?limit="+limit, ""); w.Code != http.StatusOK {
			t.Fatalf("limit=%s: status = %d, want 200", limit, w.Code)
		}
	}
}

func TestCursorRoundTrip(t *testing.T) {
	in := store.Cursor{
		// Microseconds, like Postgres timestamptz; a non-UTC zone to check normalising.
		CreatedAt: time.Date(2026, 10, 23, 14, 5, 6, 123456000, time.FixedZone("ALMT", 5*3600)),
		ID:        uuid.New(),
	}

	out, err := decodeCursor(encodeCursor(in))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.CreatedAt.Equal(in.CreatedAt) || out.ID != in.ID {
		t.Fatalf("round trip changed cursor: %+v -> %+v", in, out)
	}
	if strings.ContainsAny(encodeCursor(in), "+/=") {
		t.Fatal("cursor is not URL-safe")
	}
}

// ---- delete ----

func TestDeleteProblem(t *testing.T) {
	api := newTestAPI(t)
	created := api.create("to delete")
	path := "/problems/" + created.ID.String()

	w := api.do(http.MethodDelete, path, "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("204 must have an empty body, got %q", w.Body.String())
	}

	if w := api.do(http.MethodGet, path, ""); w.Code != http.StatusNotFound {
		t.Fatalf("get after delete: status = %d, want 404", w.Code)
	}
	if w := api.do(http.MethodDelete, path, ""); w.Code != http.StatusNotFound {
		t.Fatalf("second delete: status = %d, want 404", w.Code)
	}
}

func TestDeleteProblemMalformedID(t *testing.T) {
	api := newTestAPI(t)
	if w := api.do(http.MethodDelete, "/problems/xyz", ""); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// ---- error mapping ----

func TestStoreNotFoundSafetyNet(t *testing.T) {
	s := New(&fakePinger{}, newFakeStore(), discardLogger())
	w := httptest.NewRecorder()
	s.writeError(w, httptest.NewRequest(http.MethodGet, "/", nil), store.ErrNotFound)

	if w.Code != http.StatusNotFound {
		t.Fatalf("store.ErrNotFound mapped to %d, want 404", w.Code)
	}
}
