package lightops

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCandidateSubmissionOwnsIncomingEvidence(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	evidence := []string{"before.csv", "window.jpg"}
	value := testComplaint("candidate-01", "luohu")
	value.Evidence = evidence
	if _, err := service.SubmitComplaint(context.Background(), value, "candidate-01"); err != nil {
		t.Fatal(err)
	}
	evidence[0] = "tampered.csv"
	stored, _ := store.Complaint(context.Background(), value.ID)
	if stored.Evidence[0] != "before.csv" {
		t.Fatalf("incoming slice polluted storage: %#v", stored.Evidence)
	}
}

func TestCandidateComplaintReadIsDetached(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "candidate-02", "luohu")
	first, _ := store.Complaint(context.Background(), "candidate-02")
	first.Evidence[0] = "tampered.csv"
	second, _ := store.Complaint(context.Background(), "candidate-02")
	if second.Evidence[0] != "balcony.csv" {
		t.Fatalf("read result polluted repository: %#v", second.Evidence)
	}
}

func TestCandidateAuditFailureRollsBackComplaint(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	store.FailNextEvent()
	_, err := service.SubmitComplaint(context.Background(), testComplaint("candidate-03", "luohu"), "candidate-03")
	if !errors.Is(err, ErrStorage) {
		t.Fatalf("expected storage error: %v", err)
	}
	if _, err := store.Complaint(context.Background(), "candidate-03"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("partial complaint remained: %v", err)
	}
}

func TestCandidateFailedSubmissionKeepsRequestKeyReusable(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	store.FailNextEvent()
	value := testComplaint("candidate-04", "luohu")
	if _, err := service.SubmitComplaint(context.Background(), value, "candidate-04"); !errors.Is(err, ErrStorage) {
		t.Fatalf("expected storage error: %v", err)
	}
	created, err := service.SubmitComplaint(context.Background(), value, "candidate-04")
	if err != nil || created.ID != value.ID {
		t.Fatalf("retry could not reuse key: %#v %v", created, err)
	}
}

func TestCandidateRequestKeyIsScopedByDistrict(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	first, _ := service.SubmitComplaint(context.Background(), testComplaint("candidate-05-a", "luohu"), "mobile-05")
	second, err := service.SubmitComplaint(context.Background(), testComplaint("candidate-05-b", "nanshan"), "mobile-05")
	if err != nil || second.ID == first.ID || second.DistrictID != "nanshan" {
		t.Fatalf("district idempotency collision: %#v %#v %v", first, second, err)
	}
}

func TestCandidateRequestKeyIsScopedByAction(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	complaint, _ := service.SubmitComplaint(context.Background(), testComplaint("candidate-06", "luohu"), "shared-06")
	plan, err := service.PublishPlan(context.Background(), RectificationPlan{ID: "plan-06", ComplaintID: complaint.ID, DistrictID: "luohu", CrewID: "crew", Steps: []string{"shield"}}, "shared-06")
	if err != nil || plan.ID != "plan-06" {
		t.Fatalf("action idempotency collision: %#v %v", plan, err)
	}
}

func TestCandidatePlanOwnsIncomingSteps(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "candidate-07", "luohu")
	steps := []string{"shield", "tilt"}
	if _, err := service.PublishPlan(context.Background(), RectificationPlan{ID: "plan-07", ComplaintID: "candidate-07", DistrictID: "luohu", CrewID: "crew", Steps: steps}, "plan-07"); err != nil {
		t.Fatal(err)
	}
	steps[0] = "remove"
	stored, _ := store.Plan(context.Background(), "plan-07")
	if stored.Steps[0] != "shield" {
		t.Fatalf("plan steps polluted: %#v", stored.Steps)
	}
}

func TestCandidateScheduleOwnsRowState(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	rows := map[string]bool{"first": true, "second": false}
	created, err := service.RegisterSchedule(context.Background(), Schedule{ID: "schedule-08", DistrictID: "luohu", Rows: rows, Cutoff: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	rows["second"] = true
	created.Rows["first"] = false
	stored, _ := store.Schedule(context.Background(), "schedule-08")
	if !stored.Rows["first"] || stored.Rows["second"] {
		t.Fatalf("schedule map polluted: %#v", stored.Rows)
	}
}

func TestCandidateAssessmentOwnsReadings(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "candidate-09", "luohu")
	readings := []float64{82, 77}
	created, err := service.SaveAssessment(context.Background(), Assessment{ID: "assessment-09", ComplaintID: "candidate-09", DistrictID: "luohu", Readings: readings})
	if err != nil {
		t.Fatal(err)
	}
	readings[0] = 1
	created.Readings[1] = 2
	stored, _ := store.Assessment(context.Background(), "assessment-09")
	if stored.Readings[0] != 82 || stored.Readings[1] != 77 {
		t.Fatalf("readings polluted: %#v", stored.Readings)
	}
}

func TestCandidateRetryDoesNotDuplicatePlan(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "candidate-10", "luohu")
	store.FailNextPlan()
	plan := RectificationPlan{ID: "plan-10", ComplaintID: "candidate-10", DistrictID: "luohu", CrewID: "crew", Steps: []string{"shield"}}
	first, err := service.RetryPlanPublication(context.Background(), plan, "retry-10")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.RetryPlanPublication(context.Background(), plan, "retry-10")
	if err != nil || second.ID != first.ID || second.Version != first.Version {
		t.Fatalf("retry duplicated work: %#v %#v %v", first, second, err)
	}
}

func TestCandidateListDoesNotCrossDistrict(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "candidate-11", "luohu")
	items, err := service.ListComplaints(context.Background(), "nanshan")
	if err != nil || len(items) != 0 {
		t.Fatalf("district list leaked: %#v %v", items, err)
	}
}

func TestCandidateZoneAssignmentDoesNotCrossDistrict(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "candidate-12", "luohu")
	if err := service.AssignZone(context.Background(), "nanshan", "candidate-12", "zone-x"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-district zone write allowed: %v", err)
	}
	stored, _ := store.Complaint(context.Background(), "candidate-12")
	if stored.ZoneID != "" {
		t.Fatalf("forbidden write changed state: %#v", stored)
	}
}

func TestCandidatePlanDoesNotCrossDistrict(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "candidate-13", "luohu")
	_, err := service.PublishPlan(context.Background(), RectificationPlan{ID: "plan-13", ComplaintID: "candidate-13", DistrictID: "nanshan", CrewID: "crew", Steps: []string{"shield"}}, "plan-13")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-district plan allowed: %v", err)
	}
	if _, err := store.Plan(context.Background(), "plan-13"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("forbidden plan persisted: %v", err)
	}
}

func TestCandidateAssessmentDoesNotCrossDistrict(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "candidate-14", "luohu")
	_, err := service.SaveAssessment(context.Background(), Assessment{ID: "assessment-14", ComplaintID: "candidate-14", DistrictID: "nanshan", Readings: []float64{70}})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-district assessment allowed: %v", err)
	}
	if _, err := store.Assessment(context.Background(), "assessment-14"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("forbidden assessment persisted: %v", err)
	}
}

func TestCandidateAcceptanceDoesNotCrossDistrict(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	ready := readyComplaint(t, service, "candidate-15", "luohu")
	_, err := service.AcceptComplaint(context.Background(), "nanshan", ready.ID, ready.Version)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-district acceptance allowed: %v", err)
	}
	stored, _ := store.Complaint(context.Background(), ready.ID)
	if stored.Status != "pending" {
		t.Fatalf("forbidden acceptance changed state: %#v", stored)
	}
}

func TestCandidateVerificationDoesNotCrossDistrict(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "candidate-16", "luohu")
	plan, _ := service.PublishPlan(context.Background(), RectificationPlan{ID: "plan-16", ComplaintID: "candidate-16", DistrictID: "luohu", CrewID: "crew", Steps: []string{"shield"}}, "plan-16")
	_, err := service.VerifyPlan(context.Background(), "nanshan", plan.ID, plan.Version)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-district verification allowed: %v", err)
	}
	stored, _ := store.Plan(context.Background(), plan.ID)
	if stored.Status != "submitted" {
		t.Fatalf("forbidden verification changed plan: %#v", stored)
	}
}

func TestCandidateClosureDoesNotCrossDistrict(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	ready := readyComplaint(t, service, "candidate-17", "luohu")
	accepted, _ := service.AcceptComplaint(context.Background(), "luohu", ready.ID, ready.Version)
	plan, _ := service.PublishPlan(context.Background(), RectificationPlan{ID: "plan-17", ComplaintID: accepted.ID, DistrictID: "luohu", CrewID: "crew", Steps: []string{"shield"}}, "plan-17")
	_, _ = service.VerifyPlan(context.Background(), "luohu", plan.ID, plan.Version)
	if err := service.CloseComplaint(context.Background(), "nanshan", accepted.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-district close allowed: %v", err)
	}
	stored, _ := store.Complaint(context.Background(), accepted.ID)
	if stored.Status != "accepted" {
		t.Fatalf("forbidden close changed state: %#v", stored)
	}
}

func TestCandidateEventsDoNotCrossDistrict(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "candidate-18", "luohu")
	events, err := store.Events(context.Background(), "nanshan")
	if err != nil || len(events) != 0 {
		t.Fatalf("district events leaked: %#v %v", events, err)
	}
}

func TestCandidateReleaseDoesNotCrossDistrict(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "candidate-19", "luohu")
	_, _ = service.SaveAssessment(context.Background(), Assessment{ID: "assessment-19", ComplaintID: "candidate-19", DistrictID: "luohu", Readings: []float64{70}})
	if err := service.ReleaseAssessment(context.Background(), "nanshan", "assessment-19"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-district release allowed: %v", err)
	}
	stored, _ := store.Assessment(context.Background(), "assessment-19")
	if stored.Released {
		t.Fatalf("forbidden release changed state: %#v", stored)
	}
}

func TestCandidateCancelledSubmissionDoesNotPersist(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.SubmitComplaint(ctx, testComplaint("candidate-20", "luohu"), "candidate-20")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if _, err := store.Complaint(context.Background(), "candidate-20"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cancelled submit persisted: %v", err)
	}
}

func TestCandidateCancelledListReturnsNoData(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "candidate-21", "luohu")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	items, err := service.ListComplaints(ctx, "luohu")
	if !errors.Is(err, context.Canceled) || items != nil {
		t.Fatalf("cancelled list returned data: %#v %v", items, err)
	}
}

func TestCandidateCancelledPlanDoesNotPersist(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "candidate-22", "luohu")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.PublishPlan(ctx, RectificationPlan{ID: "plan-22", ComplaintID: "candidate-22", DistrictID: "luohu", CrewID: "crew", Steps: []string{"shield"}}, "plan-22")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("plan cancellation lost: %v", err)
	}
	if _, err := store.Plan(context.Background(), "plan-22"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cancelled plan persisted: %v", err)
	}
}

func TestCandidateCancelledAssessmentDoesNotPersist(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "candidate-23", "luohu")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.SaveAssessment(ctx, Assessment{ID: "assessment-23", ComplaintID: "candidate-23", DistrictID: "luohu", Readings: []float64{70}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("assessment cancellation lost: %v", err)
	}
	if _, err := store.Assessment(context.Background(), "assessment-23"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cancelled assessment persisted: %v", err)
	}
}

func TestCandidateCancelledZoneAssignmentDoesNotPersist(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "candidate-24", "luohu")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := service.AssignZone(ctx, "luohu", "candidate-24", "zone-24")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("zone cancellation lost: %v", err)
	}
	stored, _ := store.Complaint(context.Background(), "candidate-24")
	if stored.ZoneID != "" {
		t.Fatalf("cancelled zone assignment persisted: %#v", stored)
	}
}

func TestCandidateCancelledAcceptanceDoesNotChangeState(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	ready := readyComplaint(t, service, "candidate-25", "luohu")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.AcceptComplaint(ctx, "luohu", ready.ID, ready.Version)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("acceptance cancellation lost: %v", err)
	}
	stored, _ := store.Complaint(context.Background(), ready.ID)
	if stored.Status != "pending" {
		t.Fatalf("cancelled acceptance changed state: %#v", stored)
	}
}

func TestCandidateCancelledVerificationDoesNotChangeState(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "candidate-26", "luohu")
	plan, _ := service.PublishPlan(context.Background(), RectificationPlan{ID: "plan-26", ComplaintID: "candidate-26", DistrictID: "luohu", CrewID: "crew", Steps: []string{"shield"}}, "plan-26")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.VerifyPlan(ctx, "luohu", plan.ID, plan.Version)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("verification cancellation lost: %v", err)
	}
	stored, _ := store.Plan(context.Background(), plan.ID)
	if stored.Status != "submitted" {
		t.Fatalf("cancelled verification changed state: %#v", stored)
	}
}

func TestCandidateCancelledClosureDoesNotChangeState(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	ready := readyComplaint(t, service, "candidate-27", "luohu")
	accepted, _ := service.AcceptComplaint(context.Background(), "luohu", ready.ID, ready.Version)
	plan, _ := service.PublishPlan(context.Background(), RectificationPlan{ID: "plan-27", ComplaintID: accepted.ID, DistrictID: "luohu", CrewID: "crew", Steps: []string{"shield"}}, "plan-27")
	_, _ = service.VerifyPlan(context.Background(), "luohu", plan.ID, plan.Version)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.CloseComplaint(ctx, "luohu", accepted.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("closure cancellation lost: %v", err)
	}
	stored, _ := store.Complaint(context.Background(), accepted.ID)
	if stored.Status != "accepted" {
		t.Fatalf("cancelled closure changed state: %#v", stored)
	}
}

func TestCandidateCancelledReleaseDoesNotChangeAssessment(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	submit(t, service, "candidate-28", "luohu")
	_, _ = service.SaveAssessment(context.Background(), Assessment{ID: "assessment-28", ComplaintID: "candidate-28", DistrictID: "luohu", Readings: []float64{70}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.ReleaseAssessment(ctx, "luohu", "assessment-28"); !errors.Is(err, context.Canceled) {
		t.Fatalf("release cancellation lost: %v", err)
	}
	stored, _ := store.Assessment(context.Background(), "assessment-28")
	if stored.Released {
		t.Fatalf("cancelled release changed state: %#v", stored)
	}
}

func TestCandidateReleasePreservesNotFoundIdentity(t *testing.T) {
	service := NewService(NewStore())
	err := service.ReleaseAssessment(context.Background(), "luohu", "missing-29")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("not-found identity lost: %v", err)
	}
}

func TestCandidateWorkerPreservesCancellationAndCause(t *testing.T) {
	store := NewStore()
	service := NewService(store)
	worker := NewWorker(service)
	submit(t, service, "candidate-30", "luohu")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := worker.ProcessPlan(ctx, RectificationPlan{ID: "plan-30", ComplaintID: "candidate-30", DistrictID: "luohu", CrewID: "crew", Steps: []string{"shield"}}, "plan-30")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("worker cancellation lost: %v", err)
	}
	err = worker.ProcessPlan(context.Background(), RectificationPlan{ID: "plan-30-missing", ComplaintID: "missing", DistrictID: "luohu", CrewID: "crew", Steps: []string{"shield"}}, "plan-30-missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("worker cause lost: %v", err)
	}
}
