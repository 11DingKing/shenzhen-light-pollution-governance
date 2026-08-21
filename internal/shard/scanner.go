package shard

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
)

type ShardFile struct {
	Path       string
	EntityType string
}

type Scanner struct {
	rootDir string
}

func NewScanner(rootDir string) *Scanner {
	return &Scanner{rootDir: rootDir}
}

func (s *Scanner) ScanEntityType(ctx context.Context, entityType string) ([]ShardFile, error) {
	dir := filepath.Join(s.rootDir, "shards", entityType)
	var files []ShardFile
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil {
			if d != nil && d.IsDir() {
				return nil
			}
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		files = append(files, ShardFile{Path: path, EntityType: entityType})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func (s *Scanner) ScanAll(ctx context.Context) ([]ShardFile, error) {
	types := []string{"items", "rules", "assignments", "escalations", "audit", "batches", "failures"}
	var all []ShardFile
	for _, t := range types {
		files, err := s.ScanEntityType(ctx, t)
		if err != nil {
			return nil, err
		}
		all = append(all, files...)
	}
	return all, nil
}
