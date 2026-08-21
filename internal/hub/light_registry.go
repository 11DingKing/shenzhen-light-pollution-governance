package hub

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

var (
	ErrLightComplaintNotFound      = errors.New("spill-light complaint not found")
	ErrLightAssessmentTeamNotFound = errors.New("assessment team not found")
	ErrLightZoneNotFound           = errors.New("lighting zone not found")
	ErrLightRuleNotFound           = errors.New("operating rule not found")
	ErrLightRectificationNotFound  = errors.New("rectification submission not found")
	ErrLightAssessorPanelNotFound  = errors.New("assessor panel not found")
	ErrLightConflict               = errors.New("light-governance resource conflict")
	ErrLightInvalidState           = errors.New("invalid light-governance state")
	ErrLightCapacity               = errors.New("light-governance capacity exhausted")
	ErrLightAuthorization          = errors.New("light-governance operation is not authorized")
	ErrLightValidation             = errors.New("light-governance request validation failed")
)

type LightGovernanceRegistry struct {
	mu             sync.RWMutex
	complaints     map[string]SpillLightComplaint
	teams          map[string]AssessmentTeam
	assessments    map[string]AssessmentReservation
	zones          map[string]LightingZone
	rules          map[string]OperatingRule
	rectifications map[string]RectificationSubmission
	panels         map[string]AssessorPanel
	events         []GovernanceEvent
	idempotency    map[string]string
}

func NewLightGovernanceRegistry() *LightGovernanceRegistry {
	return &LightGovernanceRegistry{
		complaints:     make(map[string]SpillLightComplaint),
		teams:          make(map[string]AssessmentTeam),
		assessments:    make(map[string]AssessmentReservation),
		zones:          make(map[string]LightingZone),
		rules:          make(map[string]OperatingRule),
		rectifications: make(map[string]RectificationSubmission),
		panels:         make(map[string]AssessorPanel),
		idempotency:    make(map[string]string),
	}
}

func (r *LightGovernanceRegistry) AddAssessmentTeam(ctx context.Context, team AssessmentTeam) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if team.ID == "" || team.MeasurementCapacity <= 0 {
		return fmt.Errorf("%w: team", ErrLightValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.teams[team.ID]; exists {
		return fmt.Errorf("%w: team exists", ErrLightConflict)
	}
	r.teams[team.ID] = team
	return nil
}

func (r *LightGovernanceRegistry) AddZone(ctx context.Context, zone LightingZone) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if zone.ID == "" || zone.DistrictID == "" || zone.VenueKind == "" || zone.FixtureCapacity <= 0 {
		return fmt.Errorf("%w: zone", ErrLightValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.zones[zone.ID]; exists {
		return fmt.Errorf("%w: zone exists", ErrLightConflict)
	}
	r.zones[zone.ID] = zone
	return nil
}

func (r *LightGovernanceRegistry) AddRule(ctx context.Context, rule OperatingRule) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if rule.ID == "" || rule.AuthorityID == "" || rule.Capacity <= 0 || !rule.EffectiveFrom.Before(rule.EffectiveTo) {
		return fmt.Errorf("%w: rule", ErrLightValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.rules[rule.ID]; exists {
		return fmt.Errorf("%w: rule exists", ErrLightConflict)
	}
	rule.VenueKinds = slices.Clone(rule.VenueKinds)
	r.rules[rule.ID] = rule
	return nil
}

func (r *LightGovernanceRegistry) AddPanel(ctx context.Context, panel AssessorPanel) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if panel.ID == "" || panel.Capacity <= 0 {
		return fmt.Errorf("%w: panel", ErrLightValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.panels[panel.ID]; exists {
		return fmt.Errorf("%w: panel exists", ErrLightConflict)
	}
	panel.AssessorIDs = slices.Clone(panel.AssessorIDs)
	panel.SupervisorIDs = slices.Clone(panel.SupervisorIDs)
	panel.Milestones = cloneStringMap(panel.Milestones)
	r.panels[panel.ID] = panel
	return nil
}

func (r *LightGovernanceRegistry) complaint(id string) (SpillLightComplaint, bool) {
	complaint, ok := r.complaints[id]
	if !ok {
		return SpillLightComplaint{}, false
	}
	complaint.Evidence = slices.Clone(complaint.Evidence)
	return complaint, true
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func (r *LightGovernanceRegistry) Events() []GovernanceEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	output := make([]GovernanceEvent, len(r.events))
	for i, event := range r.events {
		output[i] = event
		output[i].Metadata = cloneStringMap(event.Metadata)
	}
	return output
}

func (r *LightGovernanceRegistry) Assessment(complaintID string) (AssessmentReservation, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	assessment, ok := r.assessments[complaintID]
	return assessment, ok
}

func (r *LightGovernanceRegistry) AssessmentTeam(id string) (AssessmentTeam, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	team, ok := r.teams[id]
	return team, ok
}

func (r *LightGovernanceRegistry) Complaint(id string) (SpillLightComplaint, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.complaint(id)
}

func (r *LightGovernanceRegistry) Rectification(id string) (RectificationSubmission, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rectification, ok := r.rectifications[id]
	if ok {
		rectification.Evidence = slices.Clone(rectification.Evidence)
	}
	return rectification, ok
}

func (r *LightGovernanceRegistry) Panel(id string) (AssessorPanel, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	panel, ok := r.panels[id]
	if ok {
		panel.AssessorIDs = slices.Clone(panel.AssessorIDs)
		panel.SupervisorIDs = slices.Clone(panel.SupervisorIDs)
		panel.Milestones = cloneStringMap(panel.Milestones)
	}
	return panel, ok
}

func nowOr(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}
