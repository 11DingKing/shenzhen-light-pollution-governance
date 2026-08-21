package hub

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

var lightTestNow = time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)

func newLightTestService() (*LightGovernanceService, *LightGovernanceRegistry) {
	registry := NewLightGovernanceRegistry()
	return NewLightGovernanceService(registry, func() time.Time { return lightTestNow }), registry
}

func testComplaint(id, district string) SpillLightComplaint {
	return SpillLightComplaint{ID: id, DistrictID: district, ResidentID: "operator-" + id, ResidentName: "淘金山居民 " + id, FacilityName: "翠湖文体公园足球场", VenueKind: " football_field ", Evidence: []string{"meter-readings.csv", "window-photo.jpg"}, RequestedMeasurements: 100}
}

func reserveReady(t *testing.T, service *LightGovernanceService, registry *LightGovernanceRegistry, appID string) {
	t.Helper()
	if err := registry.AddAssessmentTeam(context.Background(), AssessmentTeam{ID: "team-1", VenueKind: "罗湖", MeasurementCapacity: 500}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SubmitComplaint(context.Background(), testComplaint(appID, "district-a")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReserveAssessment(context.Background(), appID, "team-1", 100); err != nil {
		t.Fatal(err)
	}
}

func TestLightGovernanceComplaintSubmissionClonesEvidence(t *testing.T) {
	service, registry := newLightTestService()
	evidence := []string{"balcony-lux.csv", "night-photo.jpg"}
	app := testComplaint("app-clone", "district-a")
	app.Evidence = evidence
	if _, err := service.SubmitComplaint(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	evidence[0] = "tampered.pdf"
	stored, ok := registry.Complaint(app.ID)
	if !ok || stored.Evidence[0] != "balcony-lux.csv" {
		t.Fatalf("evidence leaked into stored complaint: %#v", stored.Evidence)
	}
}

func TestLightGovernanceAssessmentReservationConcurrentCapacity(t *testing.T) {
	service, registry := newLightTestService()
	if err := registry.AddAssessmentTeam(context.Background(), AssessmentTeam{ID: "team-concurrent", VenueKind: "罗湖", MeasurementCapacity: 100}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"app-c1", "app-c2"} {
		if _, err := service.SubmitComplaint(context.Background(), testComplaint(id, "district-a")); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{"app-c1", "app-c2"} {
		wg.Add(1)
		go func(complaintID string) {
			defer wg.Done()
			_, err := service.ReserveAssessment(context.Background(), complaintID, "team-concurrent", 100)
			results <- err
		}(id)
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrLightCapacity) {
			t.Fatalf("unexpected assessment result: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("expected one assessment, got %d", successes)
	}
	team, _ := registry.AssessmentTeam("team-concurrent")
	if team.ReservedMeasurements != 100 {
		t.Fatalf("allocated measurements oversold: %d", team.ReservedMeasurements)
	}
}

func TestLightGovernanceReleaseAssessmentIsIdempotent(t *testing.T) {
	service, registry := newLightTestService()
	reserveReady(t, service, registry, "app-release")
	if err := service.ReleaseAssessment(context.Background(), "app-release"); err != nil {
		t.Fatal(err)
	}
	if err := service.ReleaseAssessment(context.Background(), "app-release"); err != nil {
		t.Fatal(err)
	}
	team, _ := registry.AssessmentTeam("team-1")
	if team.ReservedMeasurements != 0 {
		t.Fatalf("release was not idempotent: %d", team.ReservedMeasurements)
	}
}

func TestLightGovernanceAcceptanceRequiresZone(t *testing.T) {
	service, registry := newLightTestService()
	reserveReady(t, service, registry, "app-zone-required")
	app, _ := registry.Complaint("app-zone-required")
	app.LightingZoneID = "zone-required"
	registry.mu.Lock()
	registry.complaints[app.ID] = app
	registry.mu.Unlock()
	if _, err := service.AcceptComplaint(context.Background(), app.ID, "reviewer-1"); !errors.Is(err, ErrLightZoneNotFound) {
		t.Fatalf("expected unavailable zone, got %v", err)
	}
}

func TestLightGovernanceAssignZoneDistrictIsolation(t *testing.T) {
	service, registry := newLightTestService()
	reserveReady(t, service, registry, "app-zone-district")
	if err := registry.AddZone(context.Background(), LightingZone{ID: "zone-district", DistrictID: "district-b", VenueKind: "FOOTBALL_FIELD", FixtureCapacity: 100, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := service.AssignZone(context.Background(), "app-zone-district", "zone-district", 10); !errors.Is(err, ErrLightAuthorization) {
		t.Fatalf("expected district isolation, got %v", err)
	}
}

func TestLightGovernanceOperatingRuleNormalizesVenueKind(t *testing.T) {
	service, registry := newLightTestService()
	if err := registry.AddRule(context.Background(), OperatingRule{ID: "rule-venueKind", AuthorityID: "provider-1", VenueKinds: []string{" football_field "}, Capacity: 1, EffectiveFrom: lightTestNow.Add(-time.Hour), EffectiveTo: lightTestNow.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SubmitComplaint(context.Background(), testComplaint("app-rule-venueKind", "district-a")); err != nil {
		t.Fatal(err)
	}
	if err := service.ApplyRule(context.Background(), "app-rule-venueKind", "rule-venueKind", lightTestNow); err != nil {
		t.Fatalf("venueKind normalization rejected valid rule: %v", err)
	}
}

func TestLightGovernanceOperatingRuleExpiryBoundary(t *testing.T) {
	service, registry := newLightTestService()
	if err := registry.AddRule(context.Background(), OperatingRule{ID: "rule-expiry", AuthorityID: "provider-1", VenueKinds: []string{"FOOTBALL_FIELD"}, Capacity: 2, EffectiveFrom: lightTestNow.Add(-time.Hour), EffectiveTo: lightTestNow}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SubmitComplaint(context.Background(), testComplaint("app-rule-expiry", "district-a")); err != nil {
		t.Fatal(err)
	}
	if err := service.ApplyRule(context.Background(), "app-rule-expiry", "rule-expiry", lightTestNow); !errors.Is(err, ErrLightCapacity) {
		t.Fatalf("expired rule was accepted: %v", err)
	}
}

func TestLightGovernanceAppliedRuleConsumesCapacity(t *testing.T) {
	service, registry := newLightTestService()
	if err := registry.AddRule(context.Background(), OperatingRule{ID: "rule-capacity", AuthorityID: "provider-1", VenueKinds: []string{"FOOTBALL_FIELD"}, Capacity: 1, EffectiveFrom: lightTestNow.Add(-time.Hour), EffectiveTo: lightTestNow.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"app-rule-1", "app-rule-2"} {
		if _, err := service.SubmitComplaint(context.Background(), testComplaint(id, "district-a")); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.ApplyRule(context.Background(), "app-rule-1", "rule-capacity", lightTestNow); err != nil {
		t.Fatal(err)
	}
	if err := service.ApplyRule(context.Background(), "app-rule-2", "rule-capacity", lightTestNow); !errors.Is(err, ErrLightCapacity) {
		t.Fatalf("rule capacity was oversold: %v", err)
	}
}

func TestLightGovernanceRectificationRequiresEvidence(t *testing.T) {
	service, registry := newLightTestService()
	if _, err := service.SubmitComplaint(context.Background(), testComplaint("app-rectification-evidence", "district-a")); err != nil {
		t.Fatal(err)
	}
	_, err := service.PublishRectification(context.Background(), RectificationSubmission{ID: "rectification-evidence", DistrictID: "district-a", CrewID: "maintenance-crew-1", ComplaintID: "app-rectification-evidence", Evidence: []string{"photo.jpg"}})
	if !errors.Is(err, ErrLightValidation) {
		t.Fatalf("expected evidence validation, got %v", err)
	}
	_ = registry
}

func TestLightGovernanceRectificationEvidenceIsIsolated(t *testing.T) {
	service, registry := newLightTestService()
	if _, err := service.SubmitComplaint(context.Background(), testComplaint("app-rectification-clone", "district-a")); err != nil {
		t.Fatal(err)
	}
	evidence := []string{"photo.jpg", "report.pdf"}
	if _, err := service.PublishRectification(context.Background(), RectificationSubmission{ID: "rectification-clone", DistrictID: "district-a", CrewID: "maintenance-crew-1", ComplaintID: "app-rectification-clone", Evidence: evidence}); err != nil {
		t.Fatal(err)
	}
	evidence[0] = "altered.jpg"
	rectification, _ := registry.Rectification("rectification-clone")
	if rectification.Evidence[0] != "photo.jpg" {
		t.Fatalf("rectification evidence was aliased: %#v", rectification.Evidence)
	}
}

func TestLightGovernanceRectificationVerificationState(t *testing.T) {
	service, registry := newLightTestService()
	if _, err := service.SubmitComplaint(context.Background(), testComplaint("app-rectification-state", "district-a")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PublishRectification(context.Background(), RectificationSubmission{ID: "rectification-state", DistrictID: "district-a", CrewID: "maintenance-crew-1", ComplaintID: "app-rectification-state", Evidence: []string{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyRectification(context.Background(), "rectification-state", "reviewer"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyRectification(context.Background(), "rectification-state", "reviewer"); !errors.Is(err, ErrLightInvalidState) {
		t.Fatalf("accepted rectification reopened: %v", err)
	}
	_ = registry
}

func TestLightGovernanceAssessorEnrollmentCapacity(t *testing.T) {
	service, registry := newLightTestService()
	if err := registry.AddPanel(context.Background(), AssessorPanel{ID: "opc-cap", Name: "罗湖照明评估专家组", Capacity: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.EnrollAssessor(context.Background(), "opc-cap", "founder-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.EnrollAssessor(context.Background(), "opc-cap", "founder-2"); !errors.Is(err, ErrLightCapacity) {
		t.Fatalf("panel exceeded capacity: %v", err)
	}
}

func TestLightGovernanceAssessorEnrollmentIdempotent(t *testing.T) {
	service, registry := newLightTestService()
	if err := registry.AddPanel(context.Background(), AssessorPanel{ID: "opc-idem", Name: "罗湖照明评估专家组", Capacity: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.EnrollAssessor(context.Background(), "opc-idem", "founder-1"); err != nil {
		t.Fatal(err)
	}
	panel, err := service.EnrollAssessor(context.Background(), "opc-idem", "founder-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(panel.AssessorIDs) != 1 {
		t.Fatalf("duplicate assessorEnrollment created a second seat: %#v", panel.AssessorIDs)
	}
}

func TestLightGovernanceResubmissionPreservesAttempt(t *testing.T) {
	service, registry := newLightTestService()
	if _, err := service.SubmitComplaint(context.Background(), testComplaint("app-resubmit", "district-a")); err != nil {
		t.Fatal(err)
	}
	registry.mu.Lock()
	app := registry.complaints["app-resubmit"]
	app.Status = ComplaintReturned
	registry.complaints[app.ID] = app
	registry.mu.Unlock()
	updated, err := service.ResubmitComplaint(context.Background(), "app-resubmit", []string{"new-plan.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Attempt != 2 || updated.Status != ComplaintPending {
		t.Fatalf("unexpected resubmission: %#v", updated)
	}
}

func TestLightGovernanceCannotClosePending(t *testing.T) {
	service, registry := newLightTestService()
	if _, err := service.SubmitComplaint(context.Background(), testComplaint("app-close", "district-a")); err != nil {
		t.Fatal(err)
	}
	if err := service.CloseComplaint(context.Background(), "app-close"); !errors.Is(err, ErrLightInvalidState) {
		t.Fatalf("pending complaint closed: %v", err)
	}
	_ = registry
}

func TestLightGovernanceListDistrictIsolation(t *testing.T) {
	service, _ := newLightTestService()
	for _, district := range []string{"district-a", "district-b"} {
		if _, err := service.SubmitComplaint(context.Background(), testComplaint("app-list-"+district, district)); err != nil {
			t.Fatal(err)
		}
	}
	result, err := service.ListComplaints(context.Background(), ComplaintQuery{DistrictID: "district-a"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Complaints[0].DistrictID != "district-a" {
		t.Fatalf("cross-district result: %#v", result)
	}
}

func TestLightGovernanceListPaginationTotal(t *testing.T) {
	service, _ := newLightTestService()
	for i := 0; i < 3; i++ {
		if _, err := service.SubmitComplaint(context.Background(), testComplaint("app-page-"+string(rune('a'+i)), "district-a")); err != nil {
			t.Fatal(err)
		}
	}
	result, err := service.ListComplaints(context.Background(), ComplaintQuery{DistrictID: "district-a", Offset: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Complaints) != 1 || result.Total != 3 {
		t.Fatalf("pagination total mismatch: %#v", result)
	}
}

func TestLightGovernanceContextCancellation(t *testing.T) {
	service, _ := newLightTestService()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.SubmitComplaint(ctx, testComplaint("app-cancelled", "district-a")); !errors.Is(err, context.Canceled) {
		t.Fatalf("context cancellation lost: %v", err)
	}
}

func TestLightGovernanceZoneCapacityIsAtomic(t *testing.T) {
	service, registry := newLightTestService()
	if err := registry.AddZone(context.Background(), LightingZone{ID: "zone-cap", DistrictID: "district-a", VenueKind: "FOOTBALL_FIELD", FixtureCapacity: 100, Active: true}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"app-zone-cap-1", "app-zone-cap-2"} {
		if _, err := service.SubmitComplaint(context.Background(), testComplaint(id, "district-a")); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{"app-zone-cap-1", "app-zone-cap-2"} {
		wg.Add(1)
		go func(complaintID string) {
			defer wg.Done()
			results <- service.AssignZone(context.Background(), complaintID, "zone-cap", 100)
		}(id)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrLightCapacity) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("zone oversold, successes=%d", success)
	}
}

func TestLightGovernanceComplaintInputNormalization(t *testing.T) {
	service, registry := newLightTestService()
	app := testComplaint("app-normalize", "district-a")
	app.ResidentName = "  社区物流  "
	if _, err := service.SubmitComplaint(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	stored, _ := registry.Complaint(app.ID)
	if stored.ResidentName != "社区物流" || stored.VenueKind != "FOOTBALL_FIELD" {
		t.Fatalf("input was not normalized: %#v", stored)
	}
}

func TestLightGovernanceComplaintDuplicateRejected(t *testing.T) {
	service, _ := newLightTestService()
	app := testComplaint("app-duplicate", "district-a")
	if _, err := service.SubmitComplaint(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SubmitComplaint(context.Background(), app); !errors.Is(err, ErrLightConflict) {
		t.Fatalf("duplicate complaint accepted: %v", err)
	}
}

func TestLightGovernanceComplaintEventAudit(t *testing.T) {
	service, registry := newLightTestService()
	if _, err := service.SubmitComplaint(context.Background(), testComplaint("app-audit", "district-a")); err != nil {
		t.Fatal(err)
	}
	events := registry.Events()
	if len(events) != 1 || events[0].Action != "complaint.submitted" || events[0].EntityID != "app-audit" {
		t.Fatalf("missing audit event: %#v", events)
	}
}

func TestLightGovernanceAssessmentTeamVersionAdvances(t *testing.T) {
	service, registry := newLightTestService()
	reserveReady(t, service, registry, "app-version")
	team, _ := registry.AssessmentTeam("team-1")
	if team.Version != 1 {
		t.Fatalf("team version did not advance: %d", team.Version)
	}
}

func TestLightGovernanceReleaseMissingAssessmentTeam(t *testing.T) {
	service, registry := newLightTestService()
	reserveReady(t, service, registry, "app-missing-team")
	registry.mu.Lock()
	delete(registry.teams, "team-1")
	registry.mu.Unlock()
	if err := service.ReleaseAssessment(context.Background(), "app-missing-team"); !errors.Is(err, ErrLightAssessmentTeamNotFound) {
		t.Fatalf("missing team was ignored: %v", err)
	}
}

func TestLightGovernanceRuleVenueKindListIsolation(t *testing.T) {
	service, registry := newLightTestService()
	venueKinds := []string{"FOOTBALL_FIELD", "TENNIS_COURT"}
	if err := registry.AddRule(context.Background(), OperatingRule{ID: "rule-list", AuthorityID: "provider", VenueKinds: venueKinds, Capacity: 2, EffectiveFrom: lightTestNow.Add(-time.Hour), EffectiveTo: lightTestNow.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	venueKinds[0] = "XX"
	if _, err := service.SubmitComplaint(context.Background(), testComplaint("app-rule-list", "district-a")); err != nil {
		t.Fatal(err)
	}
	if err := service.ApplyRule(context.Background(), "app-rule-list", "rule-list", lightTestNow); err != nil {
		t.Fatalf("rule venueKinds leaked: %v", err)
	}
}

func TestLightGovernancePanelMapIsolation(t *testing.T) {
	service, registry := newLightTestService()
	milestones := map[string]string{"phase-1": "planned"}
	if err := registry.AddPanel(context.Background(), AssessorPanel{ID: "opc-map", Name: "罗湖照明评估专家组", Capacity: 2, Milestones: milestones}); err != nil {
		t.Fatal(err)
	}
	milestones["phase-1"] = "tampered"
	panel, _ := registry.Panel("opc-map")
	if panel.Milestones["phase-1"] != "planned" {
		t.Fatalf("panel map leaked: %#v", panel.Milestones)
	}
	_ = service
}

func TestLightGovernancePendingToApprovedFlow(t *testing.T) {
	service, registry := newLightTestService()
	reserveReady(t, service, registry, "app-approve")
	if err := registry.AddZone(context.Background(), LightingZone{ID: "zone-approve", DistrictID: "district-a", VenueKind: "FOOTBALL_FIELD", FixtureCapacity: 20, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := service.AssignZone(context.Background(), "app-approve", "zone-approve", 10); err != nil {
		t.Fatal(err)
	}
	updated, err := service.AcceptComplaint(context.Background(), "app-approve", "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != ComplaintAccepted {
		t.Fatalf("complaint did not approve: %#v", updated)
	}
}

func TestLightGovernanceClosedStateTerminal(t *testing.T) {
	service, registry := newLightTestService()
	reserveReady(t, service, registry, "app-terminal")
	if _, err := service.AcceptComplaint(context.Background(), "app-terminal", "reviewer"); err != nil {
		t.Fatal(err)
	}
	registry.mu.Lock()
	app := registry.complaints["app-terminal"]
	app.Status = ComplaintActive
	registry.complaints[app.ID] = app
	registry.mu.Unlock()
	if err := service.CloseComplaint(context.Background(), "app-terminal"); err != nil {
		t.Fatal(err)
	}
	if err := service.CloseComplaint(context.Background(), "app-terminal"); !errors.Is(err, ErrLightInvalidState) {
		t.Fatalf("closed complaint reopened: %v", err)
	}
}

func TestLightGovernanceQueryStatusFilter(t *testing.T) {
	service, registry := newLightTestService()
	if _, err := service.SubmitComplaint(context.Background(), testComplaint("app-status-pending", "district-a")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SubmitComplaint(context.Background(), testComplaint("app-status-accepted", "district-a")); err != nil {
		t.Fatal(err)
	}
	registry.mu.Lock()
	app := registry.complaints["app-status-accepted"]
	app.Status = ComplaintAccepted
	registry.complaints[app.ID] = app
	registry.mu.Unlock()
	result, err := service.ListComplaints(context.Background(), ComplaintQuery{DistrictID: "district-a", Status: ComplaintAccepted})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Complaints[0].ID != "app-status-accepted" {
		t.Fatalf("status filter leaked records: %#v", result)
	}
}

func TestLightGovernanceOperatingRuleRejectsReversedWindow(t *testing.T) {
	service, registry := newLightTestService()
	err := registry.AddRule(context.Background(), OperatingRule{ID: "rule-window", AuthorityID: "provider", VenueKinds: []string{"FOOTBALL_FIELD"}, Capacity: 1, EffectiveFrom: lightTestNow.Add(time.Hour), EffectiveTo: lightTestNow})
	if !errors.Is(err, ErrLightValidation) {
		t.Fatalf("reversed service window accepted: %v", err)
	}
	_ = service
}

func TestLightGovernanceRectificationCrewMustBeMeaningful(t *testing.T) {
	service, _ := newLightTestService()
	if _, err := service.SubmitComplaint(context.Background(), testComplaint("app-rectification-crew", "district-a")); err != nil {
		t.Fatal(err)
	}
	_, err := service.PublishRectification(context.Background(), RectificationSubmission{ID: "rectification-crew", DistrictID: "district-a", CrewID: "   ", ComplaintID: "app-rectification-crew", Evidence: []string{"photo", "report"}})
	if !errors.Is(err, ErrLightValidation) {
		t.Fatalf("blank rectification crew accepted: %v", err)
	}
}
