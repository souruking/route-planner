package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/souruking/route-planner/internal/domain"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) CreateProblem(ctx context.Context, np domain.NewProblem) (domain.Problem, error) {
	p := domain.Problem{
		Name:     np.Name,
		Depot:    np.Depot,
		Stops:    np.Stops,
		Vehicles: np.Vehicles,
	}

	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO problems (name, depot_lat, depot_lon)
			VALUES ($1, $2, $3)
			RETURNING id, created_at`,
			np.Name, np.Depot.Lat, np.Depot.Lon,
		).Scan(&p.ID, &p.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert problem: %w", err)
		}

		// COPY sends all rows in one round trip, much faster than one INSERT per row.
		// Rows get increasing ids in this order, which GetProblem relies on.
		_, err = tx.CopyFrom(ctx,
			pgx.Identifier{"stops"},
			[]string{"problem_id", "external_id", "lat", "lon", "demand"},
			pgx.CopyFromSlice(len(np.Stops), func(i int) ([]any, error) {
				st := np.Stops[i]
				return []any{p.ID, st.ExternalID, st.Location.Lat, st.Location.Lon, st.Demand}, nil
			}),
		)
		if err != nil {
			return fmt.Errorf("copy stops: %w", err)
		}

		_, err = tx.CopyFrom(ctx,
			pgx.Identifier{"vehicles"},
			[]string{"problem_id", "external_id", "capacity"},
			pgx.CopyFromSlice(len(np.Vehicles), func(i int) ([]any, error) {
				v := np.Vehicles[i]
				return []any{p.ID, v.ExternalID, v.Capacity}, nil
			}),
		)
		if err != nil {
			return fmt.Errorf("copy vehicles: %w", err)
		}

		return nil
	})
	if err != nil {
		return domain.Problem{}, fmt.Errorf("create problem: %w", err)
	}

	return p, nil
}

// GetProblem loads a problem with its stops and vehicles.
// The three queries run in one read-only REPEATABLE READ transaction, so they see
// the same snapshot: a concurrent delete can't leave us with a problem but no stops.
func (s *Store) GetProblem(ctx context.Context, id uuid.UUID) (domain.Problem, error) {
	var p domain.Problem

	txOpts := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}

	err := pgx.BeginTxFunc(ctx, s.pool, txOpts, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT id, name, depot_lat, depot_lon, created_at
			FROM problems WHERE id = $1`, id,
		).Scan(&p.ID, &p.Name, &p.Depot.Lat, &p.Depot.Lon, &p.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("select problem: %w", err)
		}

		rows, _ := tx.Query(ctx, `
			SELECT external_id, lat, lon, demand
			FROM stops WHERE problem_id = $1
			ORDER BY id`, id)
		p.Stops, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Stop, error) {
			var st domain.Stop
			err := row.Scan(&st.ExternalID, &st.Location.Lat, &st.Location.Lon, &st.Demand)
			return st, err
		})
		if err != nil {
			return fmt.Errorf("select stops: %w", err)
		}

		rows, _ = tx.Query(ctx, `
			SELECT external_id, capacity
			FROM vehicles WHERE problem_id = $1
			ORDER BY id`, id)
		p.Vehicles, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Vehicle, error) {
			var v domain.Vehicle
			err := row.Scan(&v.ExternalID, &v.Capacity)
			return v, err
		})
		if err != nil {
			return fmt.Errorf("select vehicles: %w", err)
		}

		return nil
	})
	if err != nil {
		return domain.Problem{}, err
	}

	return p, nil
}

// Cursor marks a position in the newest-first problem list.
// The id breaks ties between problems created in the same microsecond.
type Cursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// ListProblems returns up to limit problems, newest first, starting after the cursor
// (nil means from the beginning). next is nil when there are no more pages.
func (s *Store) ListProblems(ctx context.Context, after *Cursor, limit int) (items []domain.ProblemSummary, next *Cursor, err error) {
	// Fetch one extra row: if it exists, there is another page.
	var rows pgx.Rows
	if after == nil {
		rows, _ = s.pool.Query(ctx, `
			SELECT id, name, created_at FROM problems
			ORDER BY created_at DESC, id DESC
			LIMIT $1`, limit+1)
	} else {
		// Row comparison matches the (created_at DESC, id DESC) index, so this is
		// an index range scan no matter how deep the page is. OFFSET would scan
		// and discard every earlier row.
		rows, _ = s.pool.Query(ctx, `
			SELECT id, name, created_at FROM problems
			WHERE (created_at, id) < ($1, $2)
			ORDER BY created_at DESC, id DESC
			LIMIT $3`, after.CreatedAt, after.ID, limit+1)
	}

	items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.ProblemSummary, error) {
		var p domain.ProblemSummary
		err := row.Scan(&p.ID, &p.Name, &p.CreatedAt)
		return p, err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("list problems: %w", err)
	}

	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1]
		next = &Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}

	return items, next, nil
}

// DeleteProblem removes a problem; stops and vehicles go with it via ON DELETE CASCADE.
func (s *Store) DeleteProblem(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM problems WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete problem: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
