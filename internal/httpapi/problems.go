package httpapi

import "net/http"

func (s *Server) createProblem(w http.ResponseWriter, r *http.Request) error {
	return errNotImplemented()
}

func (s *Server) listProblems(w http.ResponseWriter, r *http.Request) error {
	return errNotImplemented()
}

func (s *Server) getProblem(w http.ResponseWriter, r *http.Request) error {
	_ = r.PathValue("id")
	return errNotImplemented()
}

func (s *Server) deleteProblem(w http.ResponseWriter, r *http.Request) error {
	return errNotImplemented()
}
