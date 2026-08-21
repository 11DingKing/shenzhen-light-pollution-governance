package domain

import (
	"fmt"
	"time"
)

const DataVersion = 2

type ItemStatus string

const (
	StatusRegistered  ItemStatus = "registered"
	StatusAdjudicated ItemStatus = "adjudicated"
	StatusInProgress  ItemStatus = "in_progress"
	StatusReturned    ItemStatus = "returned"
	StatusEscalated   ItemStatus = "escalated"
	StatusCompleted   ItemStatus = "completed"
	StatusCancelled   ItemStatus = "cancelled"
)

func (s ItemStatus) IsTerminal() bool {
	return s == StatusCompleted || s == StatusCancelled
}

type ComplianceCase struct {
	ID              string     `json:"id"`
	ExternalRef     string     `json:"external_ref"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	OperatorName    string     `json:"operator_name"`
	OperatorContact string     `json:"operator_contact"`
	Materials       []string   `json:"materials"`
	Category        string     `json:"category"`
	Keywords        []string   `json:"keywords"`
	Status          ItemStatus `json:"status"`
	LeadDepartment  string     `json:"lead_department"`
	CoDepartments   []string   `json:"co_departments"`
	RuleVersion     int        `json:"rule_version"`
	RegisteredAt    time.Time  `json:"registered_at"`
	RegisteredBy    string     `json:"reported_by"`
	Deadline        time.Time  `json:"deadline"`
	EscalationLevel int        `json:"escalation_level"`
	StoreID         string     `json:"store_id"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	CancelledAt     *time.Time `json:"cancelled_at,omitempty"`
	CancelReason    string     `json:"cancel_reason,omitempty"`
	ShardPath       string     `json:"-"`
	ShardOffset     int64      `json:"-"`
	DataVersion     int        `json:"data_version"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

var transitionTable = map[ItemStatus][]ItemStatus{
	StatusRegistered:  {StatusAdjudicated, StatusCancelled, StatusReturned},
	StatusAdjudicated: {StatusInProgress, StatusCancelled, StatusReturned, StatusEscalated},
	StatusInProgress:  {StatusCompleted, StatusCancelled, StatusEscalated},
	StatusReturned:    {StatusRegistered, StatusCancelled},
	StatusEscalated:   {StatusInProgress, StatusCompleted, StatusCancelled},
	StatusCompleted:   {},
	StatusCancelled:   {},
}

func CanTransition(from, to ItemStatus) bool {
	allowed, ok := transitionTable[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

func ValidateTransition(from, to ItemStatus) error {
	if from == to {
		return nil
	}
	if !CanTransition(from, to) {
		return fmt.Errorf("%w: cannot transition from %s to %s", ErrInvalidTransition, from, to)
	}
	return nil
}

func (i *ComplianceCase) TransitionTo(to ItemStatus) error {
	if err := ValidateTransition(i.Status, to); err != nil {
		return err
	}
	i.Status = to
	return nil
}

func (i *ComplianceCase) IsOverdue(now time.Time) bool {
	if i.Status.IsTerminal() {
		return false
	}
	return now.After(i.Deadline)
}

func (i *ComplianceCase) Validate() error {
	if i.ExternalRef == "" {
		return ValidationError{Field: "external_ref", Message: "must not be empty"}
	}
	if i.Title == "" {
		return ValidationError{Field: "title", Message: "must not be empty"}
	}
	if i.RegisteredBy == "" {
		return ValidationError{Field: "reported_by", Message: "must not be empty"}
	}
	if i.Deadline.IsZero() {
		return ValidationError{Field: "deadline", Message: "must not be zero"}
	}
	return nil
}

type ItemFilter struct {
	Status         ItemStatus
	LeadDepartment string
	StoreID        string
	RegisteredBy   string
	From           time.Time
	To             time.Time
	OverdueOnly    bool
	PageSize       int
	PageOffset     int
}
