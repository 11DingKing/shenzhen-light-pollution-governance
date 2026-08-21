package lightops

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

type Store struct {
	mu          sync.Mutex
	complaints  map[string]Complaint
	plans       map[string]RectificationPlan
	schedules   map[string]Schedule
	assessments map[string]Assessment
	events      []Event
	idempotency map[string]OperationResult
	failEvent   bool
	failPlan    bool
}

func NewStore() *Store {
	return &Store{complaints: map[string]Complaint{}, plans: map[string]RectificationPlan{}, schedules: map[string]Schedule{}, assessments: map[string]Assessment{}, idempotency: map[string]OperationResult{}}
}

func cloneComplaint(value Complaint) Complaint {
	value.Evidence = slices.Clone(value.Evidence)
	return value
}
func clonePlan(value RectificationPlan) RectificationPlan {
	value.Steps = slices.Clone(value.Steps)
	return value
}
func cloneAssessment(value Assessment) Assessment {
	value.Readings = slices.Clone(value.Readings)
	return value
}
func cloneSchedule(value Schedule) Schedule {
	value.Rows = cloneRows(value.Rows)
	return value
}
func cloneRows(input map[string]bool) map[string]bool {
	if input == nil {
		return nil
	}
	output := make(map[string]bool, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
func cloneEvent(value Event) Event {
	if value.Metadata == nil {
		return value
	}
	value.Metadata = make(map[string]string, len(value.Metadata))
	for key, item := range value.Metadata {
		value.Metadata[key] = item
	}
	return value
}

func (s *Store) Complaint(ctx context.Context, id string) (Complaint, error) {
	if err := ctx.Err(); err != nil {
		return Complaint{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.complaints[id]
	if !ok {
		return Complaint{}, ErrNotFound
	}
	return cloneComplaint(value), nil
}

func (s *Store) Plan(ctx context.Context, id string) (RectificationPlan, error) {
	if err := ctx.Err(); err != nil {
		return RectificationPlan{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.plans[id]
	if !ok {
		return RectificationPlan{}, ErrNotFound
	}
	return clonePlan(value), nil
}

func (s *Store) Schedule(ctx context.Context, id string) (Schedule, error) {
	if err := ctx.Err(); err != nil {
		return Schedule{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.schedules[id]
	if !ok {
		return Schedule{}, ErrNotFound
	}
	return cloneSchedule(value), nil
}

func (s *Store) Assessment(ctx context.Context, id string) (Assessment, error) {
	if err := ctx.Err(); err != nil {
		return Assessment{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.assessments[id]
	if !ok {
		return Assessment{}, ErrNotFound
	}
	return cloneAssessment(value), nil
}

func (s *Store) Events(ctx context.Context, district string) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	output := make([]Event, 0)
	for _, event := range s.events {
		if event.DistrictID == district {
			output = append(output, cloneEvent(event))
		}
	}
	return output, nil
}

func (s *Store) appendEventLocked(event Event) error {
	if s.failEvent {
		s.failEvent = false
		return fmt.Errorf("%w: append audit event", ErrStorage)
	}
	s.events = append(s.events, cloneEvent(event))
	return nil
}

func (s *Store) savePlanLocked(plan RectificationPlan) error {
	if s.failPlan {
		s.failPlan = false
		return fmt.Errorf("%w: save rectification plan", ErrStorage)
	}
	s.plans[plan.ID] = clonePlan(plan)
	return nil
}

func (s *Store) FailNextEvent() { s.mu.Lock(); s.failEvent = true; s.mu.Unlock() }
func (s *Store) FailNextPlan()  { s.mu.Lock(); s.failPlan = true; s.mu.Unlock() }
