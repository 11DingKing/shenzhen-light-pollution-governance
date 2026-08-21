package domain

import "time"

type ImportBatch struct {
	ID           string    `json:"id"`
	StoreID      string    `json:"store_id"`
	BatchDate    time.Time `json:"batch_date"`
	TotalRows    int       `json:"total_rows"`
	SuccessCount int       `json:"success_count"`
	FailureCount int       `json:"failure_count"`
	ImportedAt   time.Time `json:"imported_at"`
	ShardPath    string    `json:"-"`
	DataVersion  int       `json:"data_version"`
}

type BatchRowResult struct {
	RowIndex    int    `json:"row_index"`
	ExternalRef string `json:"external_ref"`
	Success     bool   `json:"success"`
	ItemID      string `json:"item_id,omitempty"`
	Error       string `json:"error,omitempty"`
}

type BatchImportResult struct {
	BatchID      string           `json:"batch_id"`
	TotalRows    int              `json:"total_rows"`
	SuccessCount int              `json:"success_count"`
	FailureCount int              `json:"failure_count"`
	Results      []BatchRowResult `json:"results"`
}

type BatchFilter struct {
	StoreID    string
	From       time.Time
	To         time.Time
	PageSize   int
	PageOffset int
}
