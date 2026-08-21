package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"lightgovernance/internal/auditlog"
	"lightgovernance/internal/dispatch"
	"lightgovernance/internal/domain"
	"lightgovernance/internal/store"
)

type ItemService struct {
	store       store.Store
	adjudicator *dispatch.Adjudicator
	clock       domain.Clock
	deadline    time.Duration
}

func NewItemService(s store.Store, adj *dispatch.Adjudicator, clock domain.Clock, deadline time.Duration) *ItemService {
	return &ItemService{store: s, adjudicator: adj, clock: clock, deadline: deadline}
}

type RegisterItemRequest struct {
	ExternalRef     string   `json:"external_ref"`
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	OperatorName    string   `json:"operator_name"`
	OperatorContact string   `json:"operator_contact"`
	Materials       []string `json:"materials"`
	Category        string   `json:"category"`
	Keywords        []string `json:"keywords"`
	RegisteredBy    string   `json:"reported_by"`
	StoreID         string   `json:"store_id"`
}

type ModifyItemRequest struct {
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	OperatorName    string   `json:"operator_name"`
	OperatorContact string   `json:"operator_contact"`
	Materials       []string `json:"materials"`
	Category        string   `json:"category"`
	Keywords        []string `json:"keywords"`
	Actor           string   `json:"actor"`
}

func (s *ItemService) Register(ctx context.Context, req RegisterItemRequest) (*domain.ComplianceCase, error) {
	if req.ExternalRef == "" {
		return nil, fmt.Errorf("external_ref empty: %w", domain.ErrValidation)
	}
	if req.Title == "" {
		return nil, fmt.Errorf("title empty: %w", domain.ErrValidation)
	}
	if req.RegisteredBy == "" {
		return nil, fmt.Errorf("reported_by empty: %w", domain.ErrValidation)
	}

	existing, err := s.store.GetItemByExternalRef(ctx, req.ExternalRef)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, fmt.Errorf("check duplicate: %w", err)
	}
	if existing != nil {
		return existing, nil
	}

	now := s.clock.Now()
	rules, err := s.store.GetActiveRules(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("get active rules: %w", err)
	}

	item := &domain.ComplianceCase{
		ID:              uuid.NewString(),
		ExternalRef:     req.ExternalRef,
		Title:           req.Title,
		Description:     req.Description,
		OperatorName:    req.OperatorName,
		OperatorContact: req.OperatorContact,
		Materials:       req.Materials,
		Category:        req.Category,
		Keywords:        req.Keywords,
		Status:          domain.StatusRegistered,
		RegisteredAt:    now,
		RegisteredBy:    req.RegisteredBy,
		Deadline:        now.Add(s.deadline),
		StoreID:         req.StoreID,
		DataVersion:     domain.DataVersion,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	referral, err := s.adjudicator.Adjudicate(ctx, item, rules)
	if err != nil {
		return nil, fmt.Errorf("adjudicate item %s: %w", item.ID, err)
	}
	item.LeadDepartment = referral.LeadDepartment
	item.CoDepartments = referral.CoDepartments
	item.RuleVersion = referral.RuleVersion
	item.Status = domain.StatusAdjudicated

	err = s.store.WithTx(ctx, func(tx store.Tx) error {
		if err := tx.SaveItem(ctx, item); err != nil {
			return fmt.Errorf("save item: %w", err)
		}
		if err := tx.SaveAssignment(ctx, referral); err != nil {
			return fmt.Errorf("save referral: %w", err)
		}
		audit := auditlog.NewEntry(item.ID, "item", auditlog.ActionRegister, req.RegisteredBy, now, "")
		if err := tx.SaveAudit(ctx, audit); err != nil {
			return fmt.Errorf("save audit: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("register item tx: %w", err)
	}
	return item, nil
}

func (s *ItemService) StartProcessing(ctx context.Context, id, actor string) (*domain.ComplianceCase, error) {
	item, err := s.store.GetItem(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}
	now := s.clock.Now()
	if err := item.TransitionTo(domain.StatusInProgress); err != nil {
		return nil, err
	}
	item.UpdatedAt = now
	if err := s.updateItemWithAudit(ctx, item, actor, auditlog.ActionStartProcessing, ""); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *ItemService) Modify(ctx context.Context, id string, req ModifyItemRequest) (*domain.ComplianceCase, error) {
	item, err := s.store.GetItem(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}
	if item.Status.IsTerminal() {
		return nil, fmt.Errorf("item %s is %s: %w", id, item.Status, domain.ErrAlreadyCompleted)
	}
	if req.Title != "" {
		item.Title = req.Title
	}
	if req.Description != "" {
		item.Description = req.Description
	}
	if req.OperatorName != "" {
		item.OperatorName = req.OperatorName
	}
	if req.OperatorContact != "" {
		item.OperatorContact = req.OperatorContact
	}
	if req.Materials != nil {
		item.Materials = req.Materials
	}
	if req.Category != "" {
		item.Category = req.Category
	}
	if req.Keywords != nil {
		item.Keywords = req.Keywords
	}
	item.UpdatedAt = s.clock.Now()
	if err := s.updateItemWithAudit(ctx, item, req.Actor, auditlog.ActionModify, ""); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *ItemService) ReturnForCorrection(ctx context.Context, id, reason, actor string) (*domain.ComplianceCase, error) {
	item, err := s.store.GetItem(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}
	now := s.clock.Now()
	if err := item.TransitionTo(domain.StatusReturned); err != nil {
		return nil, err
	}
	item.UpdatedAt = now
	if err := s.updateItemWithAudit(ctx, item, actor, auditlog.ActionReturn, reason); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *ItemService) Resubmit(ctx context.Context, id, actor string) (*domain.ComplianceCase, error) {
	item, err := s.store.GetItem(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}
	if err := item.TransitionTo(domain.StatusRegistered); err != nil {
		return nil, err
	}
	if err := item.TransitionTo(domain.StatusAdjudicated); err != nil {
		return nil, err
	}
	item.UpdatedAt = s.clock.Now()
	if err := s.updateItemWithAudit(ctx, item, actor, auditlog.ActionResubmit, ""); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *ItemService) Cancel(ctx context.Context, id, reason, actor string) (*domain.ComplianceCase, error) {
	item, err := s.store.GetItem(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}
	now := s.clock.Now()
	if err := item.TransitionTo(domain.StatusCancelled); err != nil {
		return nil, err
	}
	item.CancelledAt = &now
	item.CancelReason = reason
	item.UpdatedAt = now
	if err := s.updateItemWithAudit(ctx, item, actor, auditlog.ActionCancel, reason); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *ItemService) Complete(ctx context.Context, id, actor string) (*domain.ComplianceCase, error) {
	item, err := s.store.GetItem(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}
	now := s.clock.Now()
	if err := item.TransitionTo(domain.StatusCompleted); err != nil {
		return nil, err
	}
	item.CompletedAt = &now
	item.UpdatedAt = now
	if err := s.updateItemWithAudit(ctx, item, actor, auditlog.ActionComplete, ""); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *ItemService) updateItemWithAudit(ctx context.Context, item *domain.ComplianceCase, actor, action, details string) error {
	now := s.clock.Now()
	if err := s.store.WithTx(ctx, func(tx store.Tx) error {
		if err := tx.UpdateItem(ctx, item); err != nil {
			return fmt.Errorf("update item: %w", err)
		}
		audit := auditlog.NewEntry(item.ID, "item", action, actor, now, details)
		if err := tx.SaveAudit(ctx, audit); err != nil {
			return fmt.Errorf("save audit: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("update item with audit: %w", err)
	}
	return nil
}
