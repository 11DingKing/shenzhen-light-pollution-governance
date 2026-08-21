package domain

import "time"

type Referral struct {
	ID             string    `json:"id"`
	ItemID         string    `json:"item_id"`
	LeadDepartment string    `json:"lead_department"`
	CoDepartments  []string  `json:"co_departments"`
	RuleVersion    int       `json:"rule_version"`
	AdjudicatedAt  time.Time `json:"adjudicated_at"`
	AdjudicatedBy  string    `json:"adjudicated_by"`
	IsCurrent      bool      `json:"is_current"`
	ShardPath      string    `json:"-"`
	DataVersion    int       `json:"data_version"`
}
