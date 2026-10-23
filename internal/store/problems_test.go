package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/souruking/route-planner/internal/domain"
)

func sampleProblem() domain.NewProblem {
	return domain.NewProblem{
		Name:  "Алматы morning run", // non-ASCII on purpose
		Depot: domain.Location{Lat: 43.238, Lon: 76.945},
		Stops: []domain.Stop{
			// Ids deliberately not alphabetical: order must follow input, not sorting.
			{ExternalID: "order-3", Location: domain.Location{Lat: 43.256, Lon: 76.928}, Demand: 5},
			{ExternalID: "order-1", Location: domain.Location{Lat: 43.221, Lon: 76.851}, Demand: 12},
			{ExternalID: "order-2", Location: domain.Location{Lat: 43.200, Lon: 76.900}, Demand: 7},
		},
		Vehicles: []domain.Vehicle{
			{ExternalID: "van-2", Capacity: 40},
			{ExternalID: "van-1", Capacity: 20},
		},
	}
}

func TestCreateAndGetProblem(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()
	in := sampleProblem()

	created, err := s.CreateProblem(ctx, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == uuid.Nil {
		t.Fatal("created problem has no id")
	}
	if time.Since(created.CreatedAt) > time.Minute {
		t.Fatalf("created_at looks wrong: %v", created.CreatedAt)
	}

	got, err := s.GetProblem(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.ID != created.ID || got.Name != in.Name || got.Depot != in.Depot {
		t.Errorf("problem fields = %+v, want name %q depot %+v", got, in.Name, in.Depot)
	}
	if !got.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("created_at = %v, want %v", got.CreatedAt, created.CreatedAt)
	}
	if !reflect.DeepEqual(got.Stops, in.Stops) {
		t.Errorf("stops = %+v\nwant %+v", got.Stops, in.Stops)
	}
	if !reflect.DeepEqual(got.Vehicles, in.Vehicles) {
		t.Errorf("vehicles = %+v\nwant %+v", got.Vehicles, in.Vehicles)
	}
}

func TestCreateProblemManyStops(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()

	in := sampleProblem()
	in.Stops = make([]domain.Stop, domain.MaxStops)
	for i := range in.Stops {
		in.Stops[i] = domain.Stop{
			ExternalID: fmt.Sprintf("s-%04d", domain.MaxStops-i), // reverse order
			Location:   domain.Location{Lat: float64(i%180) - 90, Lon: float64(i%360) - 180},
			Demand:     1,
		}
	}

	created, err := s.CreateProblem(ctx, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetProblem(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !reflect.DeepEqual(got.Stops, in.Stops) {
		t.Fatalf("got %d stops back, order or values differ", len(got.Stops))
	}
}

func TestCreateProblemIsAtomic(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	// Bypass validation: demand 0 violates the CHECK constraint on stops,
	// which fails only after the problem row was inserted.
	in := sampleProblem()
	in.Stops[2].Demand = 0

	if _, err := s.CreateProblem(ctx, in); err == nil {
		t.Fatal("expected an error from the CHECK constraint")
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM problems`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("found %d problems after a failed create; transaction did not roll back", n)
	}
}

func TestGetProblemNotFound(t *testing.T) {
	s := New(testPool(t))

	_, err := s.GetProblem(context.Background(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestDeleteProblem(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	created, err := s.CreateProblem(ctx, sampleProblem())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := s.DeleteProblem(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := s.GetProblem(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete: err = %v, want ErrNotFound", err)
	}

	// ON DELETE CASCADE must have removed the children too.
	var stops, vehicles int
	err = pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM stops), (SELECT count(*) FROM vehicles)`,
	).Scan(&stops, &vehicles)
	if err != nil {
		t.Fatal(err)
	}
	if stops != 0 || vehicles != 0 {
		t.Fatalf("orphans left: %d stops, %d vehicles", stops, vehicles)
	}

	// Deleting again reports not found.
	if err := s.DeleteProblem(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: err = %v, want ErrNotFound", err)
	}
}

func TestListProblemsEmpty(t *testing.T) {
	s := New(testPool(t))

	items, next, err := s.ListProblems(context.Background(), nil, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 0 || next != nil {
		t.Fatalf("items = %d, next = %v; want empty and nil", len(items), next)
	}
}

func TestListProblemsPagination(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	ctx := context.Background()

	const total = 25
	for i := range total {
		p := sampleProblem()
		p.Name = fmt.Sprintf("problem %d", i)
		if _, err := s.CreateProblem(ctx, p); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	// Run twice: once with distinct timestamps, once with every timestamp equal,
	// which forces the id tie-breaker to do all the work.
	for _, tc := range []struct {
		name  string
		setup string
	}{
		{"distinct timestamps", ``},
		{"identical timestamps", `UPDATE problems SET created_at = '2026-01-01T00:00:00Z'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != "" {
				if _, err := pool.Exec(ctx, tc.setup); err != nil {
					t.Fatal(err)
				}
			}

			var all []domain.ProblemSummary
			var cursor *Cursor
			pages := 0

			for {
				items, next, err := s.ListProblems(ctx, cursor, 10)
				if err != nil {
					t.Fatalf("page %d: %v", pages, err)
				}
				all = append(all, items...)
				pages++
				if next == nil {
					break
				}
				if pages > total {
					t.Fatal("pagination does not terminate")
				}
				cursor = next
			}

			if pages != 3 { // 10 + 10 + 5
				t.Errorf("pages = %d, want 3", pages)
			}
			if len(all) != total {
				t.Fatalf("got %d problems across pages, want %d", len(all), total)
			}

			seen := map[uuid.UUID]bool{}
			for i, p := range all {
				if seen[p.ID] {
					t.Fatalf("problem %s returned twice", p.ID)
				}
				seen[p.ID] = true

				if i > 0 && p.CreatedAt.After(all[i-1].CreatedAt) {
					t.Fatalf("not newest first at index %d", i)
				}
			}
		})
	}
}

func TestListProblemsExactPageHasNoNextCursor(t *testing.T) {
	s := New(testPool(t))
	ctx := context.Background()

	for range 10 {
		if _, err := s.CreateProblem(ctx, sampleProblem()); err != nil {
			t.Fatal(err)
		}
	}

	items, next, err := s.ListProblems(ctx, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 10 || next != nil {
		t.Fatalf("items = %d, next = %v; want 10 and nil (no empty last page)", len(items), next)
	}
}
