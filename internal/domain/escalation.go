package domain

import (
	"fmt"
	"time"
)

type Escalation struct {
	ID          string    `json:"id"`
	ItemID      string    `json:"item_id"`
	FromLevel   int       `json:"from_level"`
	ToLevel     int       `json:"to_level"`
	OldLeadDept string    `json:"old_lead_dept"`
	NewLeadDept string    `json:"new_lead_dept"`
	EscalatedAt time.Time `json:"escalated_at"`
	Reason      string    `json:"reason"`
	NewDeadline time.Time `json:"new_deadline"`
	ShardPath   string    `json:"-"`
	DataVersion int       `json:"data_version"`
}

func EscalationDepartmentName(baseDept string, level int) string {
	if level <= 0 {
		return baseDept
	}
	return fmt.Sprintf("%s-supervisor-l%d", baseDept, level)
}
