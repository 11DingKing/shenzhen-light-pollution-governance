package shard

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"lightgovernance/internal/domain"
)

type Location struct {
	Path   string
	Offset int64
	Size   int64
}

type Writer struct {
	rootDir      string
	clock        domain.Clock
	mu           sync.Mutex
	maxShardSize int64
	syncOnWrite  bool
}

func NewWriter(rootDir string, clock domain.Clock, maxShardSize int64, syncOnWrite bool) *Writer {
	return &Writer{
		rootDir:      rootDir,
		clock:        clock,
		maxShardSize: maxShardSize,
		syncOnWrite:  syncOnWrite,
	}
}

func (w *Writer) Append(ctx context.Context, entityType string, t time.Time, data []byte) (*Location, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	basePath := w.shardPath(entityType, t)
	dir := filepath.Dir(basePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create shard dir %s: %w", dir, err)
	}

	targetPath := basePath
	if w.maxShardSize > 0 {
		targetPath = w.rotateIfNeeded(basePath)
	}

	f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open shard %s: %w", targetPath, err)
	}
	defer f.Close()

	offset, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, fmt.Errorf("seek shard %s: %w", targetPath, err)
	}

	line := make([]byte, 0, len(data)+1)
	line = append(line, data...)
	line = append(line, '\n')

	n, err := f.Write(line)
	if err != nil {
		return nil, fmt.Errorf("write shard %s: %w", targetPath, err)
	}

	if w.syncOnWrite {
		if err := f.Sync(); err != nil {
			return nil, fmt.Errorf("sync shard %s: %w", targetPath, err)
		}
	}

	return &Location{
		Path:   targetPath,
		Offset: offset,
		Size:   int64(n),
	}, nil
}

func (w *Writer) rotateIfNeeded(basePath string) string {
	info, err := os.Stat(basePath)
	if err != nil {
		return basePath
	}
	if info.Size() < w.maxShardSize {
		return basePath
	}
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s_%03d", basePath, i)
		candInfo, err := os.Stat(candidate)
		if err != nil {
			return candidate
		}
		if candInfo.Size() < w.maxShardSize {
			return candidate
		}
	}
}

func (w *Writer) shardPath(entityType string, t time.Time) string {
	date := t.Format("2006-01-02")
	yearMonth := t.Format("2006-01")
	return filepath.Join(w.rootDir, "shards", entityType, yearMonth, date+".jsonl")
}

func (w *Writer) EnsureDirs() error {
	types := []string{"items", "rules", "assignments", "escalations", "audit", "batches", "failures"}
	for _, t := range types {
		dir := filepath.Join(w.rootDir, "shards", t)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}
	return nil
}
