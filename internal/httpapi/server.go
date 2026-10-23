package httpapi

import (
	"context"
	"log/slog"
	"net/http"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

type Server struct {
	db  Pinger
	log *slog.Logger
}

func New(db Pinger, log *slog.Logger) *Server {
	return &Server{db: db, log: log}
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
