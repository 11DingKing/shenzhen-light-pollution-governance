package store

import "errors"

var (
	ErrStoreNotReady     = errors.New("store not ready")
	ErrShardWriteFailed  = errors.New("shard write failed")
	ErrIndexCommitFailed = errors.New("index commit failed")
	ErrNoRowsAffected    = errors.New("no rows affected")
)
