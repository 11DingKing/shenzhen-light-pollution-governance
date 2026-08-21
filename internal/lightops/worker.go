package lightops

import (
	"context"
	"errors"
	"fmt"
)

type Worker struct{ service *Service }

func NewWorker(service *Service) *Worker { return &Worker{service: service} }

func (w *Worker) ProcessPlan(ctx context.Context, plan RectificationPlan, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := w.service.RetryPlanPublication(ctx, plan, key)
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrConflict) {
		return fmt.Errorf("%w: plan conflict", ErrPermanentWork)
	}
	return fmt.Errorf("process rectification plan: %w", err)
}
