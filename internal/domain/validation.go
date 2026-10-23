package domain

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// FieldError describes one invalid field. Field uses the API's JSON path,
// e.g. "stops[3].lat", so clients can map it straight to their input.
type FieldError struct {
	Field   string
	Message string
}

// ValidationError collects every problem found in one input.
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.Field + ": " + f.Message
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// maxFieldErrors stops a 1000-stop request full of mistakes from
// producing a 1000-entry response.
const maxFieldErrors = 50

// validator accumulates field errors.
type validator struct {
	errs []FieldError
}

func (v *validator) add(field, format string, args ...any) {
	if len(v.errs) < maxFieldErrors {
		v.errs = append(v.errs, FieldError{Field: field, Message: fmt.Sprintf(format, args...)})
	}
}

func (v *validator) ok() bool { return len(v.errs) == 0 }

func (v *validator) err() error {
	if v.ok() {
		return nil
	}
	return &ValidationError{Fields: v.errs}
}

// Validate checks field rules first, then feasibility rules.
// Feasibility only runs if fields are valid: summing demands that are
// negative or missing would produce misleading messages.
func (p NewProblem) Validate() error {
	var v validator

	p.validateFields(&v)
	if v.ok() {
		p.validateFeasibility(&v)
	}

	return v.err()
}

func (p NewProblem) validateFields(v *validator) {
	// Name
	name := strings.TrimSpace(p.Name)
	switch {
	case name == "":
		v.add("name", "is required")
	case utf8.RuneCountInString(name) > MaxNameLength:
		v.add("name", "must be at most %d characters", MaxNameLength)
	}

	// Depot
	validateLocation(v, "depot", p.Depot)

	// Counts
	if n := len(p.Stops); n < MinStops || n > MaxStops {
		v.add("stops", "must contain between %d and %d stops, got %d", MinStops, MaxStops, n)
	}
	if n := len(p.Vehicles); n < MinVehicles || n > MaxVehicles {
		v.add("vehicles", "must contain between %d and %d vehicles, got %d", MinVehicles, MaxVehicles, n)
	}

	// Stops
	seenStops := make(map[string]int, len(p.Stops))
	for i, s := range p.Stops {
		path := fmt.Sprintf("stops[%d]", i)
		validateExternalID(v, path, s.ExternalID, "stops", seenStops, i)
		validateLocation(v, path, s.Location)
		validateAmount(v, path+".demand", s.Demand)
	}

	// Vehicles
	seenVehicles := make(map[string]int, len(p.Vehicles))
	for i, veh := range p.Vehicles {
		path := fmt.Sprintf("vehicles[%d]", i)
		validateExternalID(v, path, veh.ExternalID, "vehicles", seenVehicles, i)
		validateAmount(v, path+".capacity", veh.Capacity)
	}
}

func (p NewProblem) validateFeasibility(v *validator) {
	maxCap, totalCap := 0, 0
	for _, veh := range p.Vehicles {
		totalCap += veh.Capacity
		maxCap = max(maxCap, veh.Capacity)
	}

	totalDemand := 0
	for i, s := range p.Stops {
		totalDemand += s.Demand
		if s.Demand > maxCap {
			v.add(fmt.Sprintf("stops[%d].demand", i),
				"%d exceeds the largest vehicle capacity (%d)", s.Demand, maxCap)
		}
	}

	if totalDemand > totalCap {
		v.add("stops", "total demand %d exceeds total fleet capacity %d", totalDemand, totalCap)
	}
}

func validateLocation(v *validator, path string, loc Location) {
	if loc.Lat < -90 || loc.Lat > 90 {
		v.add(path+".lat", "must be between -90 and 90")
	}
	if loc.Lon < -180 || loc.Lon > 180 {
		v.add(path+".lon", "must be between -180 and 180")
	}
}

func validateAmount(v *validator, path string, n int) {
	switch {
	case n <= 0:
		v.add(path, "must be greater than 0")
	case n > MaxDemandCapacity:
		v.add(path, "must be at most %d", MaxDemandCapacity)
	}
}

// validateExternalID checks the id is present, not too long, and unique within its list.
// seen maps id -> first index where it appeared.
func validateExternalID(v *validator, path, id, list string, seen map[string]int, i int) {
	switch {
	case strings.TrimSpace(id) == "":
		v.add(path+".id", "is required")
		return
	case utf8.RuneCountInString(id) > MaxExternalIDLen:
		v.add(path+".id", "must be at most %d characters", MaxExternalIDLen)
	}

	if first, dup := seen[id]; dup {
		v.add(path+".id", "duplicates %s[%d].id (%q)", list, first, id)
		return
	}
	seen[id] = i
}
