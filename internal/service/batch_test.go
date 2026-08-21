package service_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"lightgovernance/internal/domain"
	"lightgovernance/internal/service"
)

func TestBatchImport_RowLevelResults(t *testing.T) {
	_, _, batchSvc, _, _, _, ctx := setupService(t)

	result, err := batchSvc.Import(ctx, batchReq("service_station-1",
		[]string{"REF-BATCH-1", "REF-BATCH-2"},
		[]string{"Batch ComplianceCase 1", "Batch ComplianceCase 2"},
	))
	require.NoError(t, err)
	assert.Equal(t, 2, result.TotalRows)
	assert.Equal(t, 2, result.SuccessCount)
	assert.Equal(t, 0, result.FailureCount)
}

func TestBatchImport_DuplicateIdempotentRetry(t *testing.T) {
	itemSvc, _, batchSvc, _, _, _, ctx := setupService(t)

	result, err := batchSvc.Import(ctx, batchReq("service_station-1",
		[]string{"REF-DUP-1", "REF-DUP-2", "REF-DUP-1"},
		[]string{"ComplianceCase 1", "ComplianceCase 2", "Duplicate of 1"},
	))
	require.NoError(t, err)
	assert.Equal(t, 3, result.TotalRows)
	assert.Equal(t, 3, result.SuccessCount)

	loaded1, err := itemSvc.Register(ctx, registerReq("REF-DUP-1", "Re-register"))
	require.NoError(t, err)

	loaded2, err := itemSvc.Register(ctx, registerReq("REF-DUP-2", "Re-register 2"))
	require.NoError(t, err)

	assert.NotEqual(t, loaded1.ID, loaded2.ID)

	original, err := itemSvc.Register(ctx, registerReq("REF-DUP-1", "Should be idempotent"))
	require.NoError(t, err)
	assert.Equal(t, loaded1.ID, original.ID)
}

func TestBatchImport_FailureRowsDoNotAffectSuccess(t *testing.T) {
	_, _, batchSvc, _, st, _, ctx := setupService(t)

	result, err := batchSvc.Import(ctx, service.BatchImportRequest{
		StoreID: "service_station-2",
		Items: []service.RegisterItemRequest{
			{ExternalRef: "REF-OK-1", Title: "OK ComplianceCase 1", RegisteredBy: "user1"},
			{ExternalRef: "", Title: "Invalid - no ref", RegisteredBy: "user1"},
			{ExternalRef: "REF-OK-2", Title: "OK ComplianceCase 2", RegisteredBy: "user1"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 3, result.TotalRows)
	assert.Equal(t, 2, result.SuccessCount)
	assert.Equal(t, 1, result.FailureCount)

	items, _, err := st.ListItems(ctx, domain.ItemFilter{PageSize: 100})
	require.NoError(t, err)
	assert.Equal(t, 2, len(items))
}

func TestBatchExport(t *testing.T) {
	_, _, batchSvc, _, _, _, ctx := setupService(t)

	_, err := batchSvc.Import(ctx, batchReq("service_station-1",
		[]string{"REF-EXP-1", "REF-EXP-2", "REF-EXP-3"},
		[]string{"Export 1", "Export 2", "Export 3"},
	))
	require.NoError(t, err)

	items, total, err := batchSvc.Export(ctx, service.BatchExportRequest{})
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Len(t, items, 3)
}

func batchReq(service_station string, refs, titles []string) service.BatchImportRequest {
	items := make([]service.RegisterItemRequest, len(refs))
	for i := range refs {
		items[i] = service.RegisterItemRequest{
			ExternalRef:  refs[i],
			Title:        titles[i],
			RegisteredBy: "user1",
		}
	}
	return service.BatchImportRequest{StoreID: service_station, Items: items}
}

func registerReq(ref, title string) service.RegisterItemRequest {
	return service.RegisterItemRequest{
		ExternalRef:  ref,
		Title:        title,
		RegisteredBy: "user1",
	}
}
