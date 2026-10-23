package httpapi

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/souruking/route-planner/internal/domain"
	"github.com/souruking/route-planner/internal/store"
)

// fakeStore is an in-memory ProblemStore for handler tests.
// It mimics the real store's contract: ErrNotFound, newest-first order, cursors.
// The real SQL is covered by the integration tests in internal/store.
type fakeStore struct {
	mu       sync.Mutex
	problems map[uuid.UUID]domain.Problem
	now      time.Time

	err error // when set, every method returns it

	// Recorded arguments, so tests can check what the handler passed in.
	lastAfter *store.Cursor
	lastLimit int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		problems: map[uuid.UUID]domain.Problem{},
		now:      time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC),
	}
}

func (f *fakeStore) CreateProblem(ctx context.Context, np domain.NewProblem) (domain.Problem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return domain.Problem{}, f.err
	}

	f.now = f.now.Add(time.Second) // each problem is newer than the last
	p := domain.Problem{
		ID: uuid.New(), Name: np.Name, Depot: np.Depot,
		Stops: np.Stops, Vehicles: np.Vehicles, CreatedAt: f.now,
	}
	f.problems[p.ID] = p
	return p, nil
}

func (f *fakeStore) GetProblem(ctx context.Context, id uuid.UUID) (domain.Problem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return domain.Problem{}, f.err
	}

	p, ok := f.problems[id]
	if !ok {
		return domain.Problem{}, store.ErrNotFound
	}
	return p, nil
}

func (f *fakeStore) ListProblems(ctx context.Context, after *store.Cursor, limit int) ([]domain.ProblemSummary, *store.Cursor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastAfter, f.lastLimit = after, limit
	if f.err != nil {
		return nil, nil, f.err
	}

	all := make([]domain.ProblemSummary, 0, len(f.problems))
	for _, p := range f.problems {
		all = append(all, domain.ProblemSummary{ID: p.ID, Name: p.Name, CreatedAt: p.CreatedAt})
	}
	slices.SortFunc(all, func(a, b domain.ProblemSummary) int { return b.CreatedAt.Compare(a.CreatedAt) })

	if after != nil {
		i := slices.IndexFunc(all, func(p domain.ProblemSummary) bool { return p.CreatedAt.Before(after.CreatedAt) })
		if i < 0 {
			i = len(all)
		}
		all = all[i:]
	}

	var next *store.Cursor
	if len(all) > limit {
		all = all[:limit]
		last := all[limit-1]
		next = &store.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return all, next, nil
}

func (f *fakeStore) DeleteProblem(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}

	if _, ok := f.problems[id]; !ok {
		return store.ErrNotFound
	}
	delete(f.problems, id)
	return nil
}
