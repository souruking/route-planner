package domain

import (
	"time"

	"github.com/google/uuid"
)

// Limits are exported so handlers, docs and tests can refer to the same numbers.
const (
	MaxNameLength     = 200
	MaxExternalIDLen  = 100
	MinStops          = 1
	MaxStops          = 1000
	MinVehicles       = 1
	MaxVehicles       = 50
	MaxDemandCapacity = 1_000_000 // fits Postgres integer and keeps sums far from overflow
)

// Location is a point in WGS84 degrees.
type Location struct {
	Lat float64
	Lon float64
}

// Stop is a delivery stop. ExternalID is the client's own id, e.g. "order-1042".
type Stop struct {
	ExternalID string
	Location   Location
	Demand     int
}

// Vehicle is one vehicle of the fleet.
type Vehicle struct {
	ExternalID string
	Capacity   int
}

// NewProblem is everything a client submits to create a problem.
type NewProblem struct {
	Name     string
	Depot    Location
	Stops    []Stop
	Vehicles []Vehicle
}

// Problem is a stored problem: the submitted data plus server-assigned fields.
// Stops and Vehicles keep the order the client sent them in.
type Problem struct {
	ID        uuid.UUID
	Name      string
	Depot     Location
	Stops     []Stop
	Vehicles  []Vehicle
	CreatedAt time.Time
}

// ProblemSummary is the lightweight form used in list responses.
type ProblemSummary struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
}
