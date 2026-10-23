package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/souruking/route-planner/internal/domain"
	"github.com/souruking/route-planner/internal/store"
)

// ---- Request types ----
//
// These mirror the JSON API and are separate from domain types on purpose:
// the API shape (field names, nesting, "id" vs ExternalID) can change
// without touching business rules, and vice versa.

type locationJSON struct {
	// Pointers distinguish "missing" from 0: lat 0, lon 0 is a real place,
	// so a forgotten field must not silently become a valid coordinate.
	Lat *float64 `json:"lat"`
	Lon *float64 `json:"lon"`
}

type stopJSON struct {
	ID string `json:"id"`
	locationJSON
	Demand int `json:"demand"`
}

type vehicleJSON struct {
	ID       string `json:"id"`
	Capacity int    `json:"capacity"`
}

type createProblemRequest struct {
	Name     string        `json:"name"`
	Depot    *locationJSON `json:"depot"`
	Stops    []stopJSON    `json:"stops"`
	Vehicles []vehicleJSON `json:"vehicles"`
}

// toDomain converts the request and reports fields that were missing entirely.
// Zero values that are simply invalid (demand 0, empty id) are left to domain validation.
func (req createProblemRequest) toDomain() (domain.NewProblem, []domain.FieldError) {
	var missing []domain.FieldError

	p := domain.NewProblem{
		Name:     strings.TrimSpace(req.Name),
		Stops:    make([]domain.Stop, len(req.Stops)),
		Vehicles: make([]domain.Vehicle, len(req.Vehicles)),
	}

	if req.Depot == nil {
		missing = append(missing, domain.FieldError{Field: "depot", Message: "is required"})
	} else {
		p.Depot, missing = req.Depot.toDomain("depot", missing)
	}

	for i, s := range req.Stops {
		var loc domain.Location
		loc, missing = s.locationJSON.toDomain(indexPath("stops", i), missing)
		p.Stops[i] = domain.Stop{ExternalID: s.ID, Location: loc, Demand: s.Demand}
	}

	for i, v := range req.Vehicles {
		p.Vehicles[i] = domain.Vehicle{ExternalID: v.ID, Capacity: v.Capacity}
	}

	return p, missing
}

func (l locationJSON) toDomain(path string, missing []domain.FieldError) (domain.Location, []domain.FieldError) {
	var loc domain.Location
	if l.Lat == nil {
		missing = append(missing, domain.FieldError{Field: path + ".lat", Message: "is required"})
	} else {
		loc.Lat = *l.Lat
	}
	if l.Lon == nil {
		missing = append(missing, domain.FieldError{Field: path + ".lon", Message: "is required"})
	} else {
		loc.Lon = *l.Lon
	}
	return loc, missing
}

func indexPath(list string, i int) string {
	return list + "[" + strconv.Itoa(i) + "]"
}

// ---- Response types ----
//
// Separate from request types: responses have no pointers (every field is
// always present) and add server-assigned fields like id and created_at.

type locationResponse struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type stopResponse struct {
	ID     string  `json:"id"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	Demand int     `json:"demand"`
}

type vehicleResponse struct {
	ID       string `json:"id"`
	Capacity int    `json:"capacity"`
}

type problemResponse struct {
	ID        uuid.UUID         `json:"id"`
	Name      string            `json:"name"`
	Depot     locationResponse  `json:"depot"`
	Vehicles  []vehicleResponse `json:"vehicles"`
	Stops     []stopResponse    `json:"stops"`
	CreatedAt time.Time         `json:"created_at"`
}

type problemSummaryResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type listProblemsResponse struct {
	Items      []problemSummaryResponse `json:"items"`
	NextCursor *string                  `json:"next_cursor"` // null on the last page
}

func toProblemResponse(p domain.Problem) problemResponse {
	// make() rather than nil slices, so empty lists encode as [] not null.
	stops := make([]stopResponse, len(p.Stops))
	for i, st := range p.Stops {
		stops[i] = stopResponse{ID: st.ExternalID, Lat: st.Location.Lat, Lon: st.Location.Lon, Demand: st.Demand}
	}

	vehicles := make([]vehicleResponse, len(p.Vehicles))
	for i, v := range p.Vehicles {
		vehicles[i] = vehicleResponse{ID: v.ExternalID, Capacity: v.Capacity}
	}

	return problemResponse{
		ID:        p.ID,
		Name:      p.Name,
		Depot:     locationResponse{Lat: p.Depot.Lat, Lon: p.Depot.Lon},
		Vehicles:  vehicles,
		Stops:     stops,
		CreatedAt: p.CreatedAt.UTC(),
	}
}

// ---- Handlers ----

const (
	defaultListLimit = 20
	maxListLimit     = 100
)

func (s *Server) createProblem(w http.ResponseWriter, r *http.Request) error {
	var req createProblemRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}

	np, missing := req.toDomain()

	// Report missing fields and rule violations together, in one response.
	fields := missing
	var vErr *domain.ValidationError
	if err := np.Validate(); errors.As(err, &vErr) {
		fields = append(fields, vErr.Fields...)
	}
	if len(fields) > 0 {
		return &domain.ValidationError{Fields: fields}
	}

	p, err := s.problems.CreateProblem(r.Context(), np)
	if err != nil {
		return err
	}

	w.Header().Set("Location", "/problems/"+p.ID.String())
	writeJSON(w, http.StatusCreated, toProblemResponse(p))
	return nil
}

func (s *Server) getProblem(w http.ResponseWriter, r *http.Request) error {
	id, err := problemID(r)
	if err != nil {
		return err
	}

	p, err := s.problems.GetProblem(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		return errNotFound("problem")
	}
	if err != nil {
		return err
	}

	writeJSON(w, http.StatusOK, toProblemResponse(p))
	return nil
}

func (s *Server) listProblems(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()

	limit := defaultListLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxListLimit {
			return errInvalidParam("limit", fmt.Sprintf("must be an integer between 1 and %d", maxListLimit))
		}
		limit = n
	}

	var after *store.Cursor
	if v := q.Get("cursor"); v != "" {
		c, err := decodeCursor(v)
		if err != nil {
			return errInvalidParam("cursor", "is invalid; use next_cursor from a previous response")
		}
		after = &c
	}

	items, next, err := s.problems.ListProblems(r.Context(), after, limit)
	if err != nil {
		return err
	}

	resp := listProblemsResponse{Items: make([]problemSummaryResponse, len(items))}
	for i, p := range items {
		resp.Items[i] = problemSummaryResponse{ID: p.ID, Name: p.Name, CreatedAt: p.CreatedAt.UTC()}
	}
	if next != nil {
		c := encodeCursor(*next)
		resp.NextCursor = &c
	}

	writeJSON(w, http.StatusOK, resp)
	return nil
}

func (s *Server) deleteProblem(w http.ResponseWriter, r *http.Request) error {
	id, err := problemID(r)
	if err != nil {
		return err
	}

	err = s.problems.DeleteProblem(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		return errNotFound("problem")
	}
	if err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

// problemID parses the {id} path value. A malformed id can't name an existing
// problem, so it gets the same 404 as an unknown one.
func problemID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return uuid.Nil, errNotFound("problem")
	}
	return id, nil
}
