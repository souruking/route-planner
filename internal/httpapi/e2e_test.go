package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/souruking/route-planner/internal/store"
)

// TestEndToEnd runs the full create → get → list → delete flow through a real
// HTTP server and real Postgres. It catches what the fake store can't, such as
// cursor timestamps losing precision on the way through the database.
// Skipped unless TEST_DATABASE_URL is set.
func TestEndToEnd(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping end-to-end test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := store.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE problems CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	srv := httptest.NewServer(New(pool, store.New(pool), discardLogger()).Routes())
	defer srv.Close()

	call := func(method, path, body string) *http.Response {
		t.Helper()
		var req *http.Request
		if body == "" {
			req, _ = http.NewRequestWithContext(ctx, method, srv.URL+path, nil)
		} else {
			req, _ = http.NewRequestWithContext(ctx, method, srv.URL+path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}

	// Create three problems.
	var ids []string
	for range 3 {
		resp := call(http.MethodPost, "/problems", validProblemJSON)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create: status %d", resp.StatusCode)
		}
		ids = append(ids, strings.TrimPrefix(resp.Header.Get("Location"), "/problems/"))
	}

	// Follow the Location header.
	if resp := call(http.MethodGet, "/problems/"+ids[0], ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("get: status %d", resp.StatusCode)
	}

	// Page through with limit=1: three pages, each problem exactly once.
	seen := map[string]bool{}
	path := "/problems?limit=1"
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("pagination does not terminate")
		}
		resp := call(http.MethodGet, path, "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list: status %d", resp.StatusCode)
		}
		var body listProblemsResponse
		if err := decodeJSONBody(resp, &body); err != nil {
			t.Fatal(err)
		}
		for _, it := range body.Items {
			if seen[it.ID.String()] {
				t.Fatalf("problem %s listed twice", it.ID)
			}
			seen[it.ID.String()] = true
		}
		if body.NextCursor == nil {
			break
		}
		path = "/problems?limit=1&cursor=" + *body.NextCursor
	}
	if len(seen) != 3 {
		t.Fatalf("listed %d problems, want 3", len(seen))
	}

	// Delete, then confirm it's gone.
	if resp := call(http.MethodDelete, "/problems/"+ids[0], ""); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status %d", resp.StatusCode)
	}
	if resp := call(http.MethodGet, "/problems/"+ids[0], ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete: status %d", resp.StatusCode)
	}
}
