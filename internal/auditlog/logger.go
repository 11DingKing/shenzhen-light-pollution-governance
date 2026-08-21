package auditlog

import (
	"time"

	"github.com/google/uuid"

	"lightgovernance/internal/domain"
)

func NewEntry(entityID, entityType, action, actor string, timestamp time.Time, details string) *domain.AuditEntry {
	return &domain.AuditEntry{
		ID:          uuid.NewString(),
		EntityID:    entityID,
		EntityType:  entityType,
		Action:      action,
		Actor:       actor,
		Timestamp:   timestamp,
		Details:     details,
		DataVersion: domain.DataVersion,
	}
}

const (
	ActionRegister        = "register"
	ActionAdjudicate      = "adjudicate"
	ActionModify          = "modify"
	ActionReturn          = "return_for_correction"
	ActionResubmit        = "resubmit"
	ActionCancel          = "cancel"
	ActionComplete        = "complete"
	ActionStartProcessing = "start_processing"
	ActionEscalate        = "escalate"
	ActionRuleCreate      = "rule_create"
	ActionRuleSupersede   = "supersede_rule"
	ActionBatchImport     = "batch_import"
	ActionBatchExport     = "batch_export"
	ActionFailureRetry    = "failure_retry"
	ActionFailureResolve  = "failure_resolve"
	ActionRuleReevaluated = "rule_reevaluated"
)
