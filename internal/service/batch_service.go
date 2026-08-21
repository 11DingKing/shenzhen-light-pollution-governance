package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"lightgovernance/internal/auditlog"
	"lightgovernance/internal/domain"
	"lightgovernance/internal/store"
)

type BatchService struct {
	store   store.Store
	itemSvc *ItemService
	clock   domain.Clock
}

func NewBatchService(s store.Store, itemSvc *ItemService, clock domain.Clock) *BatchService {
	return &BatchService{store: s, itemSvc: itemSvc, clock: clock}
}

type BatchImportRequest struct {
	StoreID string                `json:"store_id"`
	Items   []RegisterItemRequest `json:"items"`
}

func (s *BatchService) Import(ctx context.Context, req BatchImportRequest) (*domain.BatchImportResult, error) {
	now := s.clock.Now()
	result := &domain.BatchImportResult{
		BatchID:   uuid.NewString(),
		TotalRows: len(req.Items),
	}

	for i, itemReq := range req.Items {
		item, err := s.itemSvc.Register(ctx, itemReq)
		if err != nil {
			result.Results = append(result.Results, domain.BatchRowResult{
				RowIndex:    i,
				ExternalRef: itemReq.ExternalRef,
				Error:       err.Error(),
			})
			result.FailureCount++
			continue
		}
		result.Results = append(result.Results, domain.BatchRowResult{
			RowIndex:    i,
			ExternalRef: itemReq.ExternalRef,
			Success:     true,
			ItemID:      item.ID,
		})
		result.SuccessCount++
	}

	batch := &domain.ImportBatch{
		ID:           result.BatchID,
		StoreID:      req.StoreID,
		BatchDate:    now,
		TotalRows:    result.TotalRows,
		SuccessCount: result.SuccessCount,
		FailureCount: result.FailureCount,
		ImportedAt:   now,
		DataVersion:  domain.DataVersion,
	}

	if err := s.store.WithTx(ctx, func(tx store.Tx) error {
		if err := tx.SaveBatch(ctx, batch); err != nil {
			return fmt.Errorf("save batch: %w", err)
		}
		audit := auditlog.NewEntry(batch.ID, "batch", auditlog.ActionBatchImport, req.StoreID, now,
			fmt.Sprintf("%d success, %d fail", result.SuccessCount, result.FailureCount))
		return tx.SaveAudit(ctx, audit)
	}); err != nil {
		return nil, fmt.Errorf("save import batch: %w", err)
	}

	return result, nil
}

type BatchExportRequest struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

func (s *BatchService) Export(ctx context.Context, req BatchExportRequest) ([]*domain.ComplianceCase, int, error) {
	return s.store.ListItems(ctx, domain.ItemFilter{
		From:     req.From,
		To:       req.To,
		PageSize: 1000,
	})
}
