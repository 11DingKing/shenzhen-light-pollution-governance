package lightops

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func testComplaint(id, district string) Complaint {
	return Complaint{ID: id, DistrictID: district, ResidentID: "resident-" + id, Facility: "Cuihu football field", Evidence: []string{"balcony.csv", "window.jpg"}}
}

func submit(t *testing.T, service *Service, id, district string) Complaint {
	t.Helper()
	value, err := service.SubmitComplaint(context.Background(), testComplaint(id, district), "key-"+id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func readyComplaint(t *testing.T, service *Service, id, district string) Complaint {
	t.Helper()
	value := submit(t, service, id, district)
	assessment, err := service.SaveAssessment(context.Background(), Assessment{ID: "assessment-" + id, ComplaintID: id, DistrictID: district, TeamID: "team-1", Readings: []float64{73, 68}})
	if err != nil {
		t.Fatal(err)
	}
	_ = assessment
	if err := service.AssignZone(context.Background(), district, id, "residential-side"); err != nil {
		t.Fatal(err)
	}
	value, err = service.store.Complaint(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestRuntimeComplaintEvidenceOwnership(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	evidence := []string{"balcony.csv", "window.jpg"}
	complaint := testComplaint("c-ownership", "luohu")
	complaint.Evidence = evidence
	if _, err := service.SubmitComplaint(context.Background(), complaint, "request-1"); err != nil {
		t.Fatal(err)
	}
	evidence[0] = "changed.csv"
	stored, _ := store.Complaint(context.Background(), complaint.ID)
	if stored.Evidence[0] != "balcony.csv" {
		t.Fatalf("stored evidence was polluted: %#v", stored.Evidence)
	}
	stored.Evidence[1] = "changed-again.jpg"
	again, _ := store.Complaint(context.Background(), complaint.ID)
	if again.Evidence[1] != "window.jpg" {
		t.Fatalf("repository leaked evidence ownership: %#v", again.Evidence)
	}
}

func TestRuntimeComplaintTransactionRollsBackOnAuditFailure(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	store.FailNextEvent()
	_, err := service.SubmitComplaint(context.Background(), testComplaint("c-rollback", "luohu"), "request-rollback")
	if !errors.Is(err, ErrStorage) {
		t.Fatalf("expected storage error, got %v", err)
	}
	if _, err := store.Complaint(context.Background(), "c-rollback"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("partial complaint remained: %v", err)
	}
	if _, err := service.SubmitComplaint(context.Background(), testComplaint("c-rollback", "luohu"), "request-rollback"); err != nil {
		t.Fatalf("retry after rollback failed: %v", err)
	}
}

func TestRuntimeIdempotencyIsScopedByDistrictAndAction(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	first, err := service.SubmitComplaint(context.Background(), testComplaint("c-luohu", "luohu"), "mobile-42")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.SubmitComplaint(context.Background(), testComplaint("c-nanshan", "nanshan"), "mobile-42")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || second.DistrictID != "nanshan" {
		t.Fatalf("cross-district idempotency collision: %#v %#v", first, second)
	}
	plan, err := service.PublishPlan(context.Background(), RectificationPlan{ID: "p-1", ComplaintID: first.ID, DistrictID: "luohu", CrewID: "crew", Steps: []string{"shield"}}, "mobile-42")
	if err != nil || plan.ID != "p-1" {
		t.Fatalf("cross-action idempotency collision: %#v %v", plan, err)
	}
}

func TestRuntimeDistrictIsolationAcrossReadAndWrite(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "c-private", "luohu")
	if err := service.AssignZone(context.Background(), "nanshan", "c-private", "zone-x"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-district write allowed: %v", err)
	}
	items, err := service.ListComplaints(context.Background(), "nanshan")
	if err != nil || len(items) != 0 {
		t.Fatalf("cross-district read leaked: %#v %v", items, err)
	}
}

func TestRuntimeCancelledSubmissionLeavesNoState(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.SubmitComplaint(ctx, testComplaint("c-cancel", "luohu"), "cancel-key")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if _, err := store.Complaint(context.Background(), "c-cancel"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cancelled submission persisted: %v", err)
	}
	if _, err := service.SubmitComplaint(context.Background(), testComplaint("c-cancel", "luohu"), "cancel-key"); err != nil {
		t.Fatalf("cancelled request consumed idempotency key: %v", err)
	}
}

func TestRuntimePlanOwnsStepsAndRollbackKeepsKeyReusable(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "c-plan", "luohu")
	steps := []string{"shield", "tilt"}
	store.FailNextPlan()
	_, err := service.PublishPlan(context.Background(), RectificationPlan{ID: "p-plan", ComplaintID: "c-plan", DistrictID: "luohu", CrewID: "crew", Steps: steps}, "plan-key")
	if !errors.Is(err, ErrStorage) {
		t.Fatalf("expected storage failure: %v", err)
	}
	created, err := service.PublishPlan(context.Background(), RectificationPlan{ID: "p-plan", ComplaintID: "c-plan", DistrictID: "luohu", CrewID: "crew", Steps: steps}, "plan-key")
	if err != nil {
		t.Fatal(err)
	}
	steps[0] = "remove"
	stored, _ := store.Plan(context.Background(), created.ID)
	if stored.Steps[0] != "shield" {
		t.Fatalf("plan steps were polluted: %#v", stored.Steps)
	}
}

func TestRuntimeScheduleRowsAreIsolatedBothDirections(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	rows := map[string]bool{"first": true, "second": false}
	created, err := service.RegisterSchedule(context.Background(), Schedule{ID: "schedule-1", DistrictID: "luohu", Rows: rows, Cutoff: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	rows["second"] = true
	created.Rows["first"] = false
	stored, _ := store.Schedule(context.Background(), "schedule-1")
	if !stored.Rows["first"] || stored.Rows["second"] {
		t.Fatalf("schedule ownership leaked: %#v", stored.Rows)
	}
}

func TestRuntimeAssessmentOwnershipAndDistrictGuard(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "c-assess", "luohu")
	readings := []float64{82, 77}
	if _, err := service.SaveAssessment(context.Background(), Assessment{ID: "a-1", ComplaintID: "c-assess", DistrictID: "nanshan", Readings: readings}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-district assessment allowed: %v", err)
	}
	created, err := service.SaveAssessment(context.Background(), Assessment{ID: "a-1", ComplaintID: "c-assess", DistrictID: "luohu", Readings: readings})
	if err != nil {
		t.Fatal(err)
	}
	readings[0] = 1
	created.Readings[1] = 2
	stored, _ := store.Assessment(context.Background(), "a-1")
	if stored.Readings[0] != 82 || stored.Readings[1] != 77 {
		t.Fatalf("assessment readings polluted: %#v", stored.Readings)
	}
}

func TestRuntimeAcceptanceRequiresLinkedStateAndVersion(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	initial := submit(t, service, "c-accept", "luohu")
	if _, err := service.AcceptComplaint(context.Background(), "luohu", initial.ID, initial.Version); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("unprepared complaint accepted: %v", err)
	}
	ready := readyComplaint(t, service, "c-ready", "luohu")
	if _, err := service.AcceptComplaint(context.Background(), "luohu", ready.ID, ready.Version-1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale version accepted: %v", err)
	}
	accepted, err := service.AcceptComplaint(context.Background(), "luohu", ready.ID, ready.Version)
	if err != nil || accepted.Status != "accepted" {
		t.Fatalf("ready complaint rejected: %#v %v", accepted, err)
	}
}

func TestRuntimePlanVerificationPreservesCrossEntityInvariant(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "c-verify", "luohu")
	plan, err := service.PublishPlan(context.Background(), RectificationPlan{ID: "p-verify", ComplaintID: "c-verify", DistrictID: "luohu", CrewID: "crew", Steps: []string{"shield", "measure"}}, "plan-verify")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyPlan(context.Background(), "nanshan", plan.ID, plan.Version); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-district verification allowed: %v", err)
	}
	verified, err := service.VerifyPlan(context.Background(), "luohu", plan.ID, plan.Version)
	if err != nil {
		t.Fatal(err)
	}
	complaint, _ := store.Complaint(context.Background(), "c-verify")
	if verified.Status != "verified" || complaint.PlanID != plan.ID {
		t.Fatalf("cross-entity link missing: %#v %#v", verified, complaint)
	}
}

func TestRuntimeClosureRequiresVerifiedPlan(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	ready := readyComplaint(t, service, "c-close", "luohu")
	accepted, err := service.AcceptComplaint(context.Background(), "luohu", ready.ID, ready.Version)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.CloseComplaint(context.Background(), "luohu", accepted.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("complaint without verified plan closed: %v", err)
	}
	plan, _ := service.PublishPlan(context.Background(), RectificationPlan{ID: "p-close", ComplaintID: accepted.ID, DistrictID: "luohu", CrewID: "crew", Steps: []string{"shield", "measure"}}, "close-plan")
	if _, err := service.VerifyPlan(context.Background(), "luohu", plan.ID, plan.Version); err != nil {
		t.Fatal(err)
	}
	if err := service.CloseComplaint(context.Background(), "luohu", accepted.ID); err != nil {
		t.Fatalf("verified complaint did not close: %v", err)
	}
}

func TestRuntimeSentinelErrorSurvivesReleaseChain(t *testing.T) {
	service := NewService(NewStore())
	err := service.ReleaseAssessment(context.Background(), "luohu", "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("not-found identity lost: %v", err)
	}
}

func TestRuntimeRetryDoesNotDuplicateSuccessfulPlan(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "c-retry", "luohu")
	store.FailNextPlan()
	plan := RectificationPlan{ID: "p-retry", ComplaintID: "c-retry", DistrictID: "luohu", CrewID: "crew", Steps: []string{"shield"}}
	created, err := service.RetryPlanPublication(context.Background(), plan, "retry-key")
	if err != nil || created.ID != plan.ID {
		t.Fatalf("retry failed: %#v %v", created, err)
	}
	again, err := service.RetryPlanPublication(context.Background(), plan, "retry-key")
	if err != nil || again.Version != created.Version {
		t.Fatalf("retry duplicated plan: %#v %#v %v", created, again, err)
	}
}

func TestRuntimeConcurrentIdempotentSubmissionCreatesOneComplaint(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, id := range []string{"c-concurrent-a", "c-concurrent-b"} {
		wg.Add(1)
		go func(value string) {
			defer wg.Done()
			<-start
			_, err := service.SubmitComplaint(context.Background(), testComplaint(value, "luohu"), "same-request")
			results <- err
		}(id)
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	items, _ := service.ListComplaints(context.Background(), "luohu")
	if len(items) != 1 {
		t.Fatalf("idempotent request created %d complaints", len(items))
	}
}

func TestRuntimeWorkerPreservesCancellationAndErrorIdentity(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	worker := NewWorker(service)
	submit(t, service, "c-worker", "luohu")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := worker.ProcessPlan(ctx, RectificationPlan{ID: "p-worker", ComplaintID: "c-worker", DistrictID: "luohu", CrewID: "crew", Steps: []string{"shield"}}, "worker-key")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("worker cancellation lost: %v", err)
	}
	err = worker.ProcessPlan(context.Background(), RectificationPlan{ID: "p-other", ComplaintID: "missing", DistrictID: "luohu", CrewID: "crew", Steps: []string{"shield"}}, "worker-other")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("worker error identity lost: %v", err)
	}
}
