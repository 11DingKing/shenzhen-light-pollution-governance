package service

import (
	"context"
	"fmt"

	"lightgovernance/internal/auditlog"
	"lightgovernance/internal/domain"
	"lightgovernance/internal/store"
)

type QueryService struct {
	store store.Store
	clock domain.Clock
}

func NewQueryService(s store.Store, clock domain.Clock) *QueryService {
	return &QueryService{store: s, clock: clock}
}

type ItemDetail struct {
	ComplianceCase *domain.ComplianceCase `json:"item"`
	Assignments    []*domain.Referral     `json:"assignments"`
	Escalations    []*domain.Escalation   `json:"escalations"`
	Audit          []*domain.AuditEntry   `json:"audit"`
}

func (s *QueryService) GetItemDetail(ctx context.Context, id string) (*ItemDetail, error) {
	item, err := s.store.GetItem(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}
	assignments, err := s.store.GetAssignments(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get assignments: %w", err)
	}
	escalations, err := s.store.GetEscalations(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get escalations: %w", err)
	}
	audit, _, err := s.store.ListAudit(ctx, domain.AuditFilter{EntityID: id, PageSize: 100})
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	return &ItemDetail{
		ComplianceCase: item,
		Assignments:    assignments,
		Escalations:    escalations,
		Audit:          audit,
	}, nil
}

type BacklogStats struct {
	StatusCounts map[domain.ItemStatus]int `json:"status_counts"`
	OverdueCount int                       `json:"overdue_count"`
	OverdueItems []*domain.ComplianceCase  `json:"overdue_items"`
}

func (s *QueryService) GetBacklog(ctx context.Context) (*BacklogStats, error) {
	counts, err := s.store.CountByStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("count by status: %w", err)
	}
	overdue, err := s.store.FindOverdueItems(ctx, s.clock.Now(), 999)
	if err != nil {
		return nil, fmt.Errorf("find overdue: %w", err)
	}
	return &BacklogStats{
		StatusCounts: counts,
		OverdueCount: len(overdue),
		OverdueItems: overdue,
	}, nil
}

func (s *QueryService) ListFailures(ctx context.Context) ([]*domain.PermanentFailure, error) {
	return s.store.ListFailures(ctx)
}

func (s *QueryService) RetryFailure(ctx context.Context, id, actor string) error {
	failure, err := s.store.GetFailure(ctx, id)
	if err != nil {
		return fmt.Errorf("get failure: %w", err)
	}
	now := s.clock.Now()
	failure.Status = domain.FailureStatusRetried
	failure.ResolvedAt = &now
	return s.store.WithTx(ctx, func(tx store.Tx) error {
		if err := tx.UpdateFailure(ctx, failure); err != nil {
			return fmt.Errorf("update failure: %w", err)
		}
		audit := auditlog.NewEntry(id, "failure", auditlog.ActionFailureRetry, actor, now, "")
		return tx.SaveAudit(ctx, audit)
	})
}

func (s *QueryService) ListBatches(ctx context.Context, filter domain.BatchFilter) ([]*domain.ImportBatch, int, error) {
	return s.store.ListBatches(ctx, filter)
}

func (s *QueryService) ListAudit(ctx context.Context, filter domain.AuditFilter) ([]*domain.AuditEntry, int, error) {
	return s.store.ListAudit(ctx, filter)
}

func (s *QueryService) ListItems(ctx context.Context, filter domain.ItemFilter) ([]*domain.ComplianceCase, int, error) {
	return s.store.ListItems(ctx, filter)
}

func (s *QueryService) GetItem(ctx context.Context, id string) (*domain.ComplianceCase, error) {
	return s.store.GetItem(ctx, id)
}
