package hub

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

type LightGovernanceService struct {
	registry *LightGovernanceRegistry
	now      func() time.Time
}

func NewLightGovernanceService(registry *LightGovernanceRegistry, now func() time.Time) *LightGovernanceService {
	if registry == nil {
		registry = NewLightGovernanceRegistry()
	}
	if now == nil {
		now = time.Now
	}
	return &LightGovernanceService{registry: registry, now: now}
}

func (s *LightGovernanceService) SubmitComplaint(ctx context.Context, complaint SpillLightComplaint) (SpillLightComplaint, error) {
	if err := ctx.Err(); err != nil {
		return SpillLightComplaint{}, err
	}
	if err := validateComplaintInput(complaint); err != nil {
		return SpillLightComplaint{}, err
	}
	now := nowOr(s.now())
	complaint.ResidentName = strings.TrimSpace(complaint.ResidentName)
	complaint.VenueKind = normalizeVenueKind(complaint.VenueKind)
	complaint.Status = ComplaintPending
	complaint.Attempt = 1
	complaint.CreatedAt, complaint.UpdatedAt = now, now
	complaint.Evidence = slices.Clone(complaint.Evidence)
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()
	if _, exists := s.registry.complaints[complaint.ID]; exists {
		return SpillLightComplaint{}, fmt.Errorf("%w: complaint exists", ErrLightConflict)
	}
	s.registry.complaints[complaint.ID] = complaint
	s.registry.events = append(s.registry.events, GovernanceEvent{ID: complaint.ID + "-submitted", DistrictID: complaint.DistrictID, EntityID: complaint.ID, Action: "complaint.submitted", ActorID: complaint.ResidentID, At: now})
	return cloneComplaint(complaint), nil
}

func cloneComplaint(complaint SpillLightComplaint) SpillLightComplaint {
	complaint.Evidence = slices.Clone(complaint.Evidence)
	return complaint
}

func (s *LightGovernanceService) ReserveAssessment(ctx context.Context, complaintID, teamID string, measurementCount int) (AssessmentReservation, error) {
	if err := ctx.Err(); err != nil {
		return AssessmentReservation{}, err
	}
	if measurementCount <= 0 {
		return AssessmentReservation{}, fmt.Errorf("%w: measurement count", ErrLightValidation)
	}
	now := nowOr(s.now())
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()
	complaint, ok := s.registry.complaints[complaintID]
	if !ok {
		return AssessmentReservation{}, ErrLightComplaintNotFound
	}
	team, ok := s.registry.teams[teamID]
	if !ok {
		return AssessmentReservation{}, ErrLightAssessmentTeamNotFound
	}
	if complaint.Status != ComplaintPending && complaint.Status != ComplaintReturned {
		return AssessmentReservation{}, fmt.Errorf("%w: assessment requires pending complaint", ErrLightInvalidState)
	}
	if !measurementCapacityAvailable(team, measurementCount) {
		return AssessmentReservation{}, ErrLightCapacity
	}
	if existing, exists := s.registry.assessments[complaintID]; exists && existing.ReleasedAt == nil {
		return existing, nil
	}
	team.ReservedMeasurements += measurementCount
	team.Version++
	assessment := AssessmentReservation{ComplaintID: complaintID, AssessmentTeamID: teamID, MeasurementCount: measurementCount, ReservedAt: now}
	s.registry.teams[teamID] = team
	s.registry.assessments[complaintID] = assessment
	complaint.UpdatedAt, complaint.Version = now, complaint.Version+1
	s.registry.complaints[complaintID] = complaint
	return assessment, nil
}

func (s *LightGovernanceService) ReleaseAssessment(ctx context.Context, complaintID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := nowOr(s.now())
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()
	assessment, ok := s.registry.assessments[complaintID]
	if !ok || assessment.ReleasedAt != nil {
		return nil
	}
	team, ok := s.registry.teams[assessment.AssessmentTeamID]
	if !ok {
		return ErrLightAssessmentTeamNotFound
	}
	if team.ReservedMeasurements < assessment.MeasurementCount {
		return fmt.Errorf("%w: allocated measurements underflow", ErrLightConflict)
	}
	team.ReservedMeasurements -= assessment.MeasurementCount
	team.Version++
	assessment.ReleasedAt = &now
	s.registry.teams[team.ID] = team
	s.registry.assessments[complaintID] = assessment
	return nil
}

func (s *LightGovernanceService) AcceptComplaint(ctx context.Context, complaintID, actorID string) (SpillLightComplaint, error) {
	if err := ctx.Err(); err != nil {
		return SpillLightComplaint{}, err
	}
	now := nowOr(s.now())
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()
	complaint, ok := s.registry.complaints[complaintID]
	if !ok {
		return SpillLightComplaint{}, ErrLightComplaintNotFound
	}
	if actorID == "" {
		return SpillLightComplaint{}, ErrLightAuthorization
	}
	if !validComplaintTransition(complaint.Status, ComplaintAccepted) {
		return SpillLightComplaint{}, fmt.Errorf("%w: approve from %s", ErrLightInvalidState, complaint.Status)
	}
	if _, ok := s.registry.assessments[complaintID]; !ok {
		return SpillLightComplaint{}, fmt.Errorf("%w: assessment is not reserved", ErrLightCapacity)
	}
	if complaint.LightingZoneID == "" {
		return SpillLightComplaint{}, fmt.Errorf("%w: lighting zone is required", ErrLightZoneNotFound)
	}
	zone, exists := s.registry.zones[complaint.LightingZoneID]
	if !exists || !zone.Active {
		return SpillLightComplaint{}, fmt.Errorf("%w: zone unavailable", ErrLightZoneNotFound)
	}
	complaint.Status, complaint.UpdatedAt, complaint.Version = ComplaintAccepted, now, complaint.Version+1
	s.registry.complaints[complaintID] = complaint
	s.registry.events = append(s.registry.events, GovernanceEvent{ID: complaintID + "-accepted", EntityID: complaintID, Action: "complaint.accepted", ActorID: actorID, At: now})
	return cloneComplaint(complaint), nil
}

func (s *LightGovernanceService) AssignZone(ctx context.Context, complaintID, zoneID string, fixtureCount int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if fixtureCount <= 0 {
		return fmt.Errorf("%w: zone capacity", ErrLightValidation)
	}
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()
	complaint, ok := s.registry.complaints[complaintID]
	if !ok {
		return ErrLightComplaintNotFound
	}
	zone, ok := s.registry.zones[zoneID]
	if !ok {
		return ErrLightZoneNotFound
	}
	if zone.DistrictID != complaint.DistrictID || normalizeVenueKind(zone.VenueKind) != normalizeVenueKind(complaint.VenueKind) {
		return fmt.Errorf("%w: zone district or venueKind", ErrLightAuthorization)
	}
	if !zone.Active || zone.FixtureCapacity-zone.AllocatedFixtures < fixtureCount {
		return ErrLightCapacity
	}
	zone.AllocatedFixtures += fixtureCount
	zone.Version++
	complaint.LightingZoneID, complaint.Version = zoneID, complaint.Version+1
	s.registry.zones[zoneID], s.registry.complaints[complaintID] = zone, complaint
	return nil
}

func (s *LightGovernanceService) PublishRectification(ctx context.Context, rectification RectificationSubmission) (RectificationSubmission, error) {
	if err := ctx.Err(); err != nil {
		return RectificationSubmission{}, err
	}
	if rectification.ID == "" || rectification.DistrictID == "" || strings.TrimSpace(rectification.CrewID) == "" || rectification.ComplaintID == "" {
		return RectificationSubmission{}, fmt.Errorf("%w: rectification identity", ErrLightValidation)
	}
	if len(rectification.Evidence) < 2 {
		return RectificationSubmission{}, fmt.Errorf("%w: at least two evidence items", ErrLightValidation)
	}
	rectification.CrewID = strings.TrimSpace(rectification.CrewID)
	now := nowOr(s.now())
	rectification.Status, rectification.SubmittedAt, rectification.Evidence = "submitted", now, slices.Clone(rectification.Evidence)
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()
	if _, exists := s.registry.rectifications[rectification.ID]; exists {
		return RectificationSubmission{}, fmt.Errorf("%w: rectification exists", ErrLightConflict)
	}
	if complaint, ok := s.registry.complaints[rectification.ComplaintID]; !ok || complaint.DistrictID != rectification.DistrictID {
		return RectificationSubmission{}, ErrLightComplaintNotFound
	}
	s.registry.rectifications[rectification.ID] = rectification
	return cloneRectification(rectification), nil
}

func cloneRectification(rectification RectificationSubmission) RectificationSubmission {
	rectification.Evidence = slices.Clone(rectification.Evidence)
	return rectification
}

func (s *LightGovernanceService) VerifyRectification(ctx context.Context, rectificationID, reviewerID string) (RectificationSubmission, error) {
	if err := ctx.Err(); err != nil {
		return RectificationSubmission{}, err
	}
	if reviewerID == "" {
		return RectificationSubmission{}, ErrLightAuthorization
	}
	now := nowOr(s.now())
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()
	rectification, ok := s.registry.rectifications[rectificationID]
	if !ok {
		return RectificationSubmission{}, ErrLightRectificationNotFound
	}
	if err := rectificationCanApprove(rectification); err != nil {
		return RectificationSubmission{}, err
	}
	rectification.Status, rectification.Version, rectification.ReviewedAt = "accepted", rectification.Version+1, &now
	s.registry.rectifications[rectificationID] = rectification
	return cloneRectification(rectification), nil
}

func (s *LightGovernanceService) ApplyRule(ctx context.Context, complaintID, ruleID string, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()
	complaint, ok := s.registry.complaints[complaintID]
	if !ok {
		return ErrLightComplaintNotFound
	}
	rule, ok := s.registry.rules[ruleID]
	if !ok {
		return ErrLightRuleNotFound
	}
	if !ruleAvailable(rule, complaint.VenueKind, nowOr(now)) {
		return ErrLightCapacity
	}
	rule.Used++
	rule.Version++
	complaint.OperatingRuleID, complaint.Version = ruleID, complaint.Version+1
	s.registry.rules[ruleID], s.registry.complaints[complaintID] = rule, complaint
	return nil
}

func (s *LightGovernanceService) EnrollAssessor(ctx context.Context, panelID, assessorID string) (AssessorPanel, error) {
	if err := ctx.Err(); err != nil {
		return AssessorPanel{}, err
	}
	if strings.TrimSpace(assessorID) == "" {
		return AssessorPanel{}, fmt.Errorf("%w: assessor", ErrLightValidation)
	}
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()
	panel, ok := s.registry.panels[panelID]
	if !ok {
		return AssessorPanel{}, ErrLightAssessorPanelNotFound
	}
	for _, existing := range panel.AssessorIDs {
		if existing == assessorID {
			return clonePanel(panel), nil
		}
	}
	if len(panel.AssessorIDs) >= panel.Capacity {
		return AssessorPanel{}, ErrLightCapacity
	}
	panel.AssessorIDs = append(panel.AssessorIDs, assessorID)
	panel.Version++
	s.registry.panels[panelID] = panel
	return clonePanel(panel), nil
}

func clonePanel(panel AssessorPanel) AssessorPanel {
	panel.AssessorIDs = slices.Clone(panel.AssessorIDs)
	panel.SupervisorIDs = slices.Clone(panel.SupervisorIDs)
	panel.Milestones = cloneStringMap(panel.Milestones)
	return panel
}

func (s *LightGovernanceService) ResubmitComplaint(ctx context.Context, complaintID string, evidence []string) (SpillLightComplaint, error) {
	if err := ctx.Err(); err != nil {
		return SpillLightComplaint{}, err
	}
	if len(evidence) == 0 {
		return SpillLightComplaint{}, fmt.Errorf("%w: resubmission evidence", ErrLightValidation)
	}
	now := nowOr(s.now())
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()
	complaint, ok := s.registry.complaints[complaintID]
	if !ok {
		return SpillLightComplaint{}, ErrLightComplaintNotFound
	}
	if complaint.Status != ComplaintReturned {
		return SpillLightComplaint{}, fmt.Errorf("%w: resubmit from %s", ErrLightInvalidState, complaint.Status)
	}
	complaint.Status, complaint.Attempt, complaint.Evidence, complaint.UpdatedAt, complaint.Version = ComplaintPending, complaint.Attempt+1, slices.Clone(evidence), now, complaint.Version+1
	s.registry.complaints[complaintID] = complaint
	return cloneComplaint(complaint), nil
}

func (s *LightGovernanceService) CloseComplaint(ctx context.Context, complaintID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := nowOr(s.now())
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()
	complaint, ok := s.registry.complaints[complaintID]
	if !ok {
		return ErrLightComplaintNotFound
	}
	if !validComplaintTransition(complaint.Status, ComplaintClosed) {
		return fmt.Errorf("%w: close from %s", ErrLightInvalidState, complaint.Status)
	}
	complaint.Status, complaint.UpdatedAt, complaint.Version = ComplaintClosed, now, complaint.Version+1
	s.registry.complaints[complaintID] = complaint
	return nil
}

func (s *LightGovernanceService) ListComplaints(ctx context.Context, query ComplaintQuery) (ComplaintQueryResult, error) {
	if err := ctx.Err(); err != nil {
		return ComplaintQueryResult{}, err
	}
	if query.Offset < 0 || query.Limit < 0 || query.Limit > 100 {
		return ComplaintQueryResult{}, fmt.Errorf("%w: pagination", ErrLightValidation)
	}
	s.registry.mu.RLock()
	defer s.registry.mu.RUnlock()
	items := make([]SpillLightComplaint, 0)
	for _, complaint := range s.registry.complaints {
		if query.DistrictID != "" && complaint.DistrictID != query.DistrictID {
			continue
		}
		if query.Status != "" && complaint.Status != query.Status {
			continue
		}
		if query.VenueKind != "" && normalizeVenueKind(complaint.VenueKind) != normalizeVenueKind(query.VenueKind) {
			continue
		}
		items = append(items, cloneComplaint(complaint))
	}
	if query.Offset >= len(items) {
		return ComplaintQueryResult{Complaints: []SpillLightComplaint{}, Total: len(items)}, nil
	}
	end := len(items)
	if query.Limit > 0 && query.Offset+query.Limit < end {
		end = query.Offset + query.Limit
	}
	return ComplaintQueryResult{Complaints: slices.Clone(items[query.Offset:end]), Total: len(items)}, nil
}
