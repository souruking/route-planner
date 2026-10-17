package api

import "net/http"

func (s *Server) createProblem(w http.ResponseWriter, r *http.Request) { notImplemented(w) }
func (s *Server) listProblems(w http.ResponseWriter, r *http.Request)  { notImplemented(w) }
func (s *Server) getProblem(w http.ResponseWriter, r *http.Request) {
	_ = r.PathValue("id")
	notImplemented(w)
}
func (s *Server) deleteProblem(w http.ResponseWriter, r *http.Request) { notImplemented(w) }

func notImplemented(w http.ResponseWriter) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
