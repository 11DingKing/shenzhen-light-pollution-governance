package lightops

import (
	"errors"
	"time"
)

var (
	ErrNotFound      = errors.New("light governance record not found")
	ErrConflict      = errors.New("light governance conflict")
	ErrForbidden     = errors.New("cross-district access forbidden")
	ErrInvalidState  = errors.New("invalid light governance state")
	ErrCapacity      = errors.New("assessment capacity exhausted")
	ErrStorage       = errors.New("light governance storage failure")
	ErrPermanentWork = errors.New("permanent light governance worker failure")
)

type Complaint struct {
	ID, DistrictID, ResidentID, Facility string
	Status                               string
	Evidence                             []string
	ZoneID, AssessmentID, PlanID         string
	Version                              int
}

type RectificationPlan struct {
	ID, DistrictID, ComplaintID, CrewID string
	Status                              string
	Steps                               []string
	Version                             int
}

type Schedule struct {
	ID, DistrictID string
	Rows           map[string]bool
	Cutoff         time.Time
	Version        int
}

type Assessment struct {
	ID, DistrictID, ComplaintID, TeamID string
	Readings                            []float64
	Released                            bool
	Version                             int
}

type Event struct {
	ID, DistrictID, EntityID, Action string
	Metadata                         map[string]string
}

type OperationResult struct {
	EntityID string
	Version  int
}
