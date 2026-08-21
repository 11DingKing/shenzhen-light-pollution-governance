package shard

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShardWriter_AppendAndSync(t *testing.T) {
	dir := t.TempDir()
	clk := clock.NewMock()
	w := NewWriter(dir, clk, 0, true)
	ctx := context.Background()

	loc, err := w.Append(ctx, "items", clk.Now(), []byte(`{"id":"1","name":"test"}`))
	require.NoError(t, err)
	assert.NotEmpty(t, loc.Path)
	assert.Equal(t, int64(len(`{"id":"1","name":"test"}`)+1), loc.Size)

	loc2, err := w.Append(ctx, "items", clk.Now(), []byte(`{"id":"2"}`))
	require.NoError(t, err)
	assert.Equal(t, loc.Path, loc2.Path)
	assert.Greater(t, loc2.Offset, loc.Offset)
}

func TestShardReader_ReadBack(t *testing.T) {
	dir := t.TempDir()
	clk := clock.NewMock()
	w := NewWriter(dir, clk, 0, true)
	r := NewReader()
	ctx := context.Background()

	_, err := w.Append(ctx, "items", clk.Now(), []byte(`{"id":"1"}`))
	require.NoError(t, err)
	_, err = w.Append(ctx, "items", clk.Now(), []byte(`{"id":"2"}`))
	require.NoError(t, err)

	clk.Add(24 * time.Hour)
	_, err = w.Append(ctx, "items", clk.Now(), []byte(`{"id":"3"}`))
	require.NoError(t, err)

	scanner := NewScanner(dir)
	files, err := scanner.ScanEntityType(ctx, "items")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(files), 2)

	for _, f := range files {
		records, err := r.ReadAll(f.Path)
		require.NoError(t, err)
		assert.Greater(t, len(records), 0)
	}
}

func TestShard_ChecksumAndCount(t *testing.T) {
	dir := t.TempDir()
	clk := clock.NewMock()
	w := NewWriter(dir, clk, 0, true)
	r := NewReader()
	ctx := context.Background()

	loc, err := w.Append(ctx, "items", clk.Now(), []byte(`{"id":"1"}`))
	require.NoError(t, err)

	checksum, err := r.Checksum(loc.Path)
	require.NoError(t, err)
	assert.NotEmpty(t, checksum)

	count, err := r.RecordCount(loc.Path)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestShard_ShardRotationBySize(t *testing.T) {
	dir := t.TempDir()
	clk := clock.NewMock()
	w := NewWriter(dir, clk, 5, true)
	ctx := context.Background()

	loc1, err := w.Append(ctx, "items", clk.Now(), []byte(`{"id":"1"}`))
	require.NoError(t, err)
	loc2, err := w.Append(ctx, "items", clk.Now(), []byte(`{"id":"2"}`))
	require.NoError(t, err)

	assert.NotEqual(t, loc1.Path, loc2.Path)
}

func TestShard_CorruptedFileReadError(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(dir + "/test.jsonl")
	require.NoError(t, err)
	_, err = f.WriteString("line1\nline2\n")
	require.NoError(t, err)
	f.Close()

	r := NewReader()
	records, err := r.ReadAll(dir + "/test.jsonl")
	require.NoError(t, err)
	assert.Len(t, records, 2)
}
