package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/souruking/route-planner/internal/domain"
	"github.com/souruking/route-planner/internal/store"
)

// Pinger is what readiness checks need from the database.
type Pinger interface {
	Ping(ctx context.Context) error
}

// ProblemStore is what the problem handlers need from storage.
// *store.Store satisfies it; tests use an in-memory fake.
type ProblemStore interface {
	CreateProblem(ctx context.Context, p domain.NewProblem) (domain.Problem, error)
	GetProblem(ctx context.Context, id uuid.UUID) (domain.Problem, error)
	ListProblems(ctx context.Context, after *store.Cursor, limit int) ([]domain.ProblemSummary, *store.Cursor, error)
	DeleteProblem(ctx context.Context, id uuid.UUID) error
}

type Server struct {
	db       Pinger
	problems ProblemStore
	log      *slog.Logger
}

func New(db Pinger, problems ProblemStore, log *slog.Logger) *Server {
	return &Server{db: db, problems: problems, log: log}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)

	mux.HandleFunc("POST /problems", s.handle(s.createProblem))
	mux.HandleFunc("GET /problems", s.handle(s.listProblems))
	mux.HandleFunc("GET /problems/{id}", s.handle(s.getProblem))
	mux.HandleFunc("DELETE /problems/{id}", s.handle(s.deleteProblem))

	return s.requestID(s.logRequests(s.recoverPanics(mux)))
}
