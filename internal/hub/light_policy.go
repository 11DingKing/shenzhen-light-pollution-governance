package hub

import (
	"fmt"
	"strings"
	"time"
)

var allowedVenueKinds = map[string]struct{}{
	"FOOTBALL_FIELD":   {},
	"BASKETBALL_COURT": {},
	"TENNIS_COURT":     {},
	"RUNNING_TRACK":    {},
}

func validateComplaintInput(complaint SpillLightComplaint) error {
	if complaint.ID == "" || complaint.DistrictID == "" || complaint.ResidentID == "" || strings.TrimSpace(complaint.ResidentName) == "" {
		return fmt.Errorf("%w: identity", ErrLightValidation)
	}
	if _, ok := allowedVenueKinds[normalizeVenueKind(complaint.VenueKind)]; !ok {
		return fmt.Errorf("%w: venue kind", ErrLightValidation)
	}
	if complaint.RequestedMeasurements <= 0 || complaint.RequestedMeasurements > 5000 {
		return fmt.Errorf("%w: requested measurements", ErrLightValidation)
	}
	if len(complaint.Evidence) == 0 {
		return fmt.Errorf("%w: evidence", ErrLightValidation)
	}
	return nil
}

func validComplaintTransition(from, to ComplaintStatus) bool {
	switch from {
	case ComplaintPending:
		return to == ComplaintAccepted || to == ComplaintReturned || to == ComplaintCancelled
	case ComplaintReturned:
		return to == ComplaintPending || to == ComplaintCancelled
	case ComplaintAccepted:
		return to == ComplaintActive || to == ComplaintCancelled
	case ComplaintActive:
		return to == ComplaintClosed
	case ComplaintClosed, ComplaintCancelled:
		return false
	default:
		return false
	}
}

func normalizeVenueKind(venueKind string) string {
	return strings.ToUpper(strings.TrimSpace(venueKind))
}

func ruleAvailable(rule OperatingRule, venueKind string, at time.Time) bool {
	if at.Before(rule.EffectiveFrom) || !at.Before(rule.EffectiveTo) {
		return false
	}
	if rule.Used >= rule.Capacity {
		return false
	}
	for _, candidate := range rule.VenueKinds {
		if normalizeVenueKind(candidate) == normalizeVenueKind(venueKind) {
			return true
		}
	}
	return false
}

func rectificationCanApprove(rectification RectificationSubmission) error {
	if rectification.Status != "submitted" {
		return fmt.Errorf("%w: rectification status %s", ErrLightInvalidState, rectification.Status)
	}
	if strings.TrimSpace(rectification.CrewID) == "" || len(rectification.Evidence) < 2 {
		return fmt.Errorf("%w: rectification evidence", ErrLightValidation)
	}
	return nil
}

func measurementCapacityAvailable(team AssessmentTeam, requested int) bool {
	return requested > 0 && team.MeasurementCapacity-team.ReservedMeasurements >= requested
}
