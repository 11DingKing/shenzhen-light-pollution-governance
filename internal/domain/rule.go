package domain

import "time"

type RuleStatus string

const (
	RuleStatusDraft      RuleStatus = "draft"
	RuleStatusActive     RuleStatus = "active"
	RuleStatusSuperseded RuleStatus = "superseded"
)

type Rule struct {
	Version        int        `json:"version"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	MatchKeywords  []string   `json:"match_keywords"`
	MatchCategory  string     `json:"match_category"`
	LeadDepartment string     `json:"lead_department"`
	CoDepartments  []string   `json:"co_departments"`
	Priority       int        `json:"priority"`
	IsDefault      bool       `json:"is_default"`
	EffectiveFrom  time.Time  `json:"effective_from"`
	EffectiveTo    time.Time  `json:"effective_to,omitempty"`
	Status         RuleStatus `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	CreatedBy      string     `json:"created_by"`
	ShardPath      string     `json:"-"`
	DataVersion    int        `json:"data_version"`
}

func (r *Rule) IsActiveAt(t time.Time) bool {
	if r.Status != RuleStatusActive {
		return false
	}
	if t.Before(r.EffectiveFrom) {
		return false
	}
	if !r.EffectiveTo.IsZero() && !t.Before(r.EffectiveTo) {
		return false
	}
	return true
}

func (r *Rule) Matches(item *ComplianceCase) bool {
	if r.IsDefault {
		return true
	}
	matched := false
	if r.MatchCategory != "" {
		if item.Category == r.MatchCategory {
			matched = true
		} else {
			return false
		}
	}
	if len(r.MatchKeywords) > 0 {
		kwMatched := false
		itemKW := make(map[string]bool)
		for _, k := range item.Keywords {
			itemKW[k] = true
		}
		for _, k := range r.MatchKeywords {
			if itemKW[k] {
				kwMatched = true
				break
			}
		}
		if !kwMatched {
			return false
		}
		matched = true
	}
	return matched
}

func (r *Rule) Validate() error {
	if r.Name == "" {
		return ValidationError{Field: "name", Message: "must not be empty"}
	}
	if r.LeadDepartment == "" {
		return ValidationError{Field: "lead_department", Message: "must not be empty"}
	}
	if r.EffectiveFrom.IsZero() {
		return ValidationError{Field: "effective_from", Message: "must not be zero"}
	}
	if !r.EffectiveTo.IsZero() && !r.EffectiveTo.After(r.EffectiveFrom) {
		return ValidationError{Field: "effective_to", Message: "must be after effective_from"}
	}
	return nil
}
