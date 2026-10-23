package domain

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// validProblem returns a problem that passes every rule.
// Each test case starts from it and breaks exactly one thing.
func validProblem() NewProblem {
	return NewProblem{
		Name:  "Almaty morning run",
		Depot: Location{Lat: 43.238, Lon: 76.945},
		Stops: []Stop{
			{ExternalID: "order-1", Location: Location{Lat: 43.256, Lon: 76.928}, Demand: 5},
			{ExternalID: "order-2", Location: Location{Lat: 43.221, Lon: 76.851}, Demand: 12},
		},
		Vehicles: []Vehicle{
			{ExternalID: "van-1", Capacity: 40},
			{ExternalID: "van-2", Capacity: 20},
		},
	}
}

func manyStops(n int) []Stop {
	stops := make([]Stop, n)
	for i := range stops {
		stops[i] = Stop{ExternalID: fmt.Sprintf("s%d", i), Location: Location{Lat: 1, Lon: 1}, Demand: 1}
	}
	return stops
}

func manyVehicles(n int) []Vehicle {
	vs := make([]Vehicle, n)
	for i := range vs {
		vs[i] = Vehicle{ExternalID: fmt.Sprintf("v%d", i), Capacity: 1}
	}
	return vs
}

func TestValidateValid(t *testing.T) {
	if err := validProblem().Validate(); err != nil {
		t.Fatalf("valid problem rejected: %v", err)
	}
}

func TestValidateRules(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(p *NewProblem)
		wantField string // field of the (first) expected error
		wantMsg   string // substring of its message
	}{
		// Name
		{"empty name", func(p *NewProblem) { p.Name = "" }, "name", "required"},
		{"whitespace name", func(p *NewProblem) { p.Name = "   " }, "name", "required"},
		{"long name", func(p *NewProblem) { p.Name = strings.Repeat("a", MaxNameLength+1) }, "name", "at most"},

		// Depot
		{"depot lat too high", func(p *NewProblem) { p.Depot.Lat = 90.1 }, "depot.lat", "between -90 and 90"},
		{"depot lon too low", func(p *NewProblem) { p.Depot.Lon = -180.1 }, "depot.lon", "between -180 and 180"},

		// Counts
		{"no stops", func(p *NewProblem) { p.Stops = nil }, "stops", "between"},
		{"too many stops", func(p *NewProblem) {
			p.Stops = manyStops(MaxStops + 1)
			p.Vehicles = []Vehicle{{ExternalID: "big", Capacity: MaxDemandCapacity}}
		}, "stops", "between"},
		{"no vehicles", func(p *NewProblem) { p.Vehicles = nil }, "vehicles", "between"},
		{"too many vehicles", func(p *NewProblem) { p.Vehicles = manyVehicles(MaxVehicles + 1) }, "vehicles", "between"},

		// Stops
		{"stop lat out of range", func(p *NewProblem) { p.Stops[1].Location.Lat = -91 }, "stops[1].lat", "between"},
		{"stop missing id", func(p *NewProblem) { p.Stops[0].ExternalID = "" }, "stops[0].id", "required"},
		{"stop id too long", func(p *NewProblem) { p.Stops[0].ExternalID = strings.Repeat("x", MaxExternalIDLen+1) }, "stops[0].id", "at most"},
		{"duplicate stop id", func(p *NewProblem) { p.Stops[1].ExternalID = "order-1" }, "stops[1].id", "duplicates stops[0].id"},
		{"zero demand", func(p *NewProblem) { p.Stops[0].Demand = 0 }, "stops[0].demand", "greater than 0"},
		{"negative demand", func(p *NewProblem) { p.Stops[0].Demand = -3 }, "stops[0].demand", "greater than 0"},
		{"huge demand", func(p *NewProblem) { p.Stops[0].Demand = MaxDemandCapacity + 1 }, "stops[0].demand", "at most"},

		// Vehicles
		{"vehicle missing id", func(p *NewProblem) { p.Vehicles[1].ExternalID = " " }, "vehicles[1].id", "required"},
		{"duplicate vehicle id", func(p *NewProblem) { p.Vehicles[1].ExternalID = "van-1" }, "vehicles[1].id", "duplicates vehicles[0].id"},
		{"zero capacity", func(p *NewProblem) { p.Vehicles[0].Capacity = 0 }, "vehicles[0].capacity", "greater than 0"},

		// Feasibility
		{"stop larger than any vehicle", func(p *NewProblem) { p.Stops[0].Demand = 41 }, "stops[0].demand", "largest vehicle capacity (40)"},
		{"total demand over fleet", func(p *NewProblem) {
			p.Stops[0].Demand = 40
			p.Stops[1].Demand = 30 // 70 > 40+20
		}, "stops", "total demand 70 exceeds total fleet capacity 60"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := validProblem()
			tt.mutate(&p)

			fields := mustValidationFields(t, p.Validate())
			if fields[0].Field != tt.wantField || !strings.Contains(fields[0].Message, tt.wantMsg) {
				t.Fatalf("got %+v, want field %q with message containing %q", fields, tt.wantField, tt.wantMsg)
			}
		})
	}
}

func TestValidateBoundaryValuesAreValid(t *testing.T) {
	p := validProblem()
	p.Name = strings.Repeat("ä", MaxNameLength) // counts characters, not bytes
	p.Depot = Location{Lat: 90, Lon: -180}
	p.Stops[0].Location = Location{Lat: -90, Lon: 180}
	p.Stops[0].Demand = 40 // equals the largest capacity
	p.Stops[1].Demand = 20 // total 60 equals fleet capacity

	if err := p.Validate(); err != nil {
		t.Fatalf("boundary values rejected: %v", err)
	}
}

func TestValidateCollectsAllErrors(t *testing.T) {
	p := validProblem()
	p.Name = ""
	p.Depot.Lat = 100
	p.Stops[0].Demand = 0
	p.Vehicles[1].ExternalID = ""

	fields := mustValidationFields(t, p.Validate())
	if len(fields) != 4 {
		t.Fatalf("got %d errors, want 4: %+v", len(fields), fields)
	}
}

func TestValidateSkipsFeasibilityWhenFieldsInvalid(t *testing.T) {
	p := validProblem()
	p.Name = ""
	p.Stops[0].Demand = 1000 // would also fail feasibility

	fields := mustValidationFields(t, p.Validate())
	for _, f := range fields {
		if strings.Contains(f.Message, "capacity") {
			t.Fatalf("feasibility ran despite field errors: %+v", fields)
		}
	}
}

func TestValidateCapsErrorCount(t *testing.T) {
	p := validProblem()
	p.Stops = manyStops(MaxStops)
	for i := range p.Stops {
		p.Stops[i].Demand = 0
	}

	fields := mustValidationFields(t, p.Validate())
	if len(fields) != maxFieldErrors {
		t.Fatalf("got %d errors, want cap of %d", len(fields), maxFieldErrors)
	}
}

func mustValidationFields(t *testing.T, err error) []FieldError {
	t.Helper()
	var vErr *ValidationError
	if !errors.As(err, &vErr) {
		t.Fatalf("want *ValidationError, got %T: %v", err, err)
	}
	if len(vErr.Fields) == 0 {
		t.Fatal("ValidationError has no fields")
	}
	return vErr.Fields
}
