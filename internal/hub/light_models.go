package hub

import "time"

type ComplaintStatus string

const (
	ComplaintPending   ComplaintStatus = "pending"
	ComplaintAccepted  ComplaintStatus = "accepted"
	ComplaintActive    ComplaintStatus = "active"
	ComplaintReturned  ComplaintStatus = "returned"
	ComplaintClosed    ComplaintStatus = "closed"
	ComplaintCancelled ComplaintStatus = "cancelled"
)

type SpillLightComplaint struct {
	ID                    string
	DistrictID            string
	ResidentID            string
	ResidentName          string
	FacilityName          string
	VenueKind             string
	Status                ComplaintStatus
	Evidence              []string
	RequestedMeasurements int
	LightingZoneID        string
	OperatingRuleID       string
	RuleVersion           int
	Attempt               int
	Deadline              time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
	Version               int
}

type AssessmentReservation struct {
	ComplaintID      string
	AssessmentTeamID string
	MeasurementCount int
	ReservedAt       time.Time
	ReleasedAt       *time.Time
}

type AssessmentTeam struct {
	ID                   string
	VenueKind            string
	MeasurementCapacity  int
	ReservedMeasurements int
	Version              int
}

type LightingZone struct {
	ID                string
	DistrictID        string
	VenueKind         string
	FixtureCapacity   int
	AllocatedFixtures int
	CutoffGrace       time.Duration
	Active            bool
	Version           int
}

type OperatingRule struct {
	ID            string
	AuthorityID   string
	Kind          string
	VenueKinds    []string
	Capacity      int
	Used          int
	EffectiveFrom time.Time
	EffectiveTo   time.Time
	Version       int
}

type RectificationSubmission struct {
	ID          string
	DistrictID  string
	CrewID      string
	PlanType    string
	Status      string
	Evidence    []string
	ComplaintID string
	SubmittedAt time.Time
	ReviewedAt  *time.Time
	Version     int
}

type AssessorPanel struct {
	ID            string
	Name          string
	Capacity      int
	AssessorIDs   []string
	SupervisorIDs []string
	Milestones    map[string]string
	Version       int
}

type GovernanceEvent struct {
	ID         string
	DistrictID string
	EntityID   string
	Action     string
	ActorID    string
	At         time.Time
	Metadata   map[string]string
}

type ComplaintQuery struct {
	DistrictID string
	Status     ComplaintStatus
	VenueKind  string
	Offset     int
	Limit      int
}

type ComplaintQueryResult struct {
	Complaints []SpillLightComplaint
	Total      int
}
