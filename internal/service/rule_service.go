package service

import (
	"context"
	"fmt"
	"strconv"

	"lightgovernance/internal/auditlog"
	"lightgovernance/internal/domain"
	"lightgovernance/internal/store"
)

type RuleService struct {
	store store.Store
	clock domain.Clock
}

func NewRuleService(s store.Store, clock domain.Clock) *RuleService {
	return &RuleService{store: s, clock: clock}
}

type CreateRuleRequest struct {
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	MatchKeywords    []string `json:"match_keywords"`
	MatchCategory    string   `json:"match_category"`
	LeadDepartment   string   `json:"lead_department"`
	CoDepartments    []string `json:"co_departments"`
	Priority         int      `json:"priority"`
	IsDefault        bool     `json:"is_default"`
	CreatedBy        string   `json:"created_by"`
	SupersedeVersion int      `json:"supersede_version,omitempty"`
}

func (s *RuleService) CreateRule(ctx context.Context, req CreateRuleRequest) (*domain.Rule, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("rule name empty: %w", domain.ErrValidation)
	}
	if req.LeadDepartment == "" {
		return nil, fmt.Errorf("lead_department empty: %w", domain.ErrValidation)
	}
	if req.CreatedBy == "" {
		return nil, fmt.Errorf("created_by empty: %w", domain.ErrValidation)
	}

	currentVersion, err := s.store.GetCurrentRuleVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("get current version: %w", err)
	}

	now := s.clock.Now()
	rule := &domain.Rule{
		Version:        currentVersion + 1,
		Name:           req.Name,
		Description:    req.Description,
		MatchKeywords:  req.MatchKeywords,
		MatchCategory:  req.MatchCategory,
		LeadDepartment: req.LeadDepartment,
		CoDepartments:  req.CoDepartments,
		Priority:       req.Priority,
		IsDefault:      req.IsDefault,
		EffectiveFrom:  now,
		Status:         domain.RuleStatusActive,
		CreatedAt:      now,
		CreatedBy:      req.CreatedBy,
		DataVersion:    domain.DataVersion,
	}

	err = s.store.WithTx(ctx, func(tx store.Tx) error {
		if req.SupersedeVersion > 0 {
			if err := tx.SupersedeRule(ctx, req.SupersedeVersion); err != nil {
				return fmt.Errorf("supersede rule v%d: %w", req.SupersedeVersion, err)
			}
			supAudit := auditlog.NewEntry(
				strconv.Itoa(req.SupersedeVersion), "rule", auditlog.ActionRuleSupersede,
				req.CreatedBy, now, fmt.Sprintf("superseded by v%d", rule.Version))
			if err := tx.SaveAudit(ctx, supAudit); err != nil {
				return fmt.Errorf("save supersede audit: %w", err)
			}
		}
		if err := tx.SaveRule(ctx, rule); err != nil {
			return fmt.Errorf("save rule: %w", err)
		}
		audit := auditlog.NewEntry(
			strconv.Itoa(rule.Version), "rule", auditlog.ActionRuleCreate,
			req.CreatedBy, now, rule.Name)
		return tx.SaveAudit(ctx, audit)
	})
	if err != nil {
		return nil, err
	}
	return rule, nil
}

func (s *RuleService) ListRules(ctx context.Context) ([]*domain.Rule, error) {
	return s.store.ListRules(ctx)
}

func (s *RuleService) GetRule(ctx context.Context, version int) (*domain.Rule, error) {
	return s.store.GetRule(ctx, version)
}

func (s *RuleService) GetActiveRules(ctx context.Context) ([]*domain.Rule, error) {
	return s.store.GetActiveRules(ctx, s.clock.Now())
}
