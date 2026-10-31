package usagereporter

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestZCodeReadActiveWALAndNormalize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.sqlite")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`PRAGMA journal_mode=WAL; CREATE TABLE model_usage (
		id TEXT PRIMARY KEY, session_id TEXT, provider_id TEXT, model_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER,
		input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cache_creation_input_tokens INTEGER,
		cache_read_input_tokens INTEGER, computed_total_tokens INTEGER, raw_usage_json TEXT);
		INSERT INTO model_usage VALUES ('id1','session','provider','model','completed',?,?,100,20,5,10,30,120,'secret');`, time.Now().UnixMilli(), time.Now().UnixMilli())
	require.NoError(t, err)
	_, err = os.Stat(path + "-wal")
	require.NoError(t, err)
	rows, err := readZCodeUsagePage(context.Background(), path, 0, 100)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	event, pending, err := normalizeZCodeUsage(rows[0])
	require.NoError(t, err)
	require.False(t, pending)
	require.Equal(t, 100, event.InputTokens)
	require.Equal(t, 30, event.CachedInputTokens)
	require.Equal(t, 5, event.ReasoningOutputTokens)
	require.Equal(t, 120, event.TotalTokens)
	rows[0].ID = "another attempt"
	other, _, err := normalizeZCodeUsage(rows[0])
	require.NoError(t, err)
	require.NotEqual(t, event.EventID, other.EventID)
}

func TestZCodeReadMissingDatabaseDoesNotCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.sqlite")
	_, err := readZCodeUsagePage(context.Background(), path, 0, 100)
	require.Error(t, err)
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))
}

func TestZCodeInvalidNumericDoesNotHideOtherRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.sqlite")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE model_usage (id TEXT PRIMARY KEY, session_id TEXT, provider_id TEXT, model_id TEXT, status TEXT, started_at INTEGER, completed_at INTEGER, input_tokens, output_tokens, reasoning_tokens, cache_creation_input_tokens, cache_read_input_tokens, computed_total_tokens);
		INSERT INTO model_usage VALUES ('a','s','p','m','completed',?,?,18446744073709551615,5,0,0,0,15);
		INSERT INTO model_usage VALUES ('b','s','p','m','completed',?,?,10,5,0,0,0,15)`, time.Now().UnixMilli(), time.Now().UnixMilli(), time.Now().UnixMilli(), time.Now().UnixMilli())
	require.NoError(t, err)
	rows, err := readZCodeUsagePage(context.Background(), path, 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	_, _, err = normalizeZCodeUsage(rows[0])
	require.Error(t, err)
	event, pending, err := normalizeZCodeUsage(rows[1])
	require.NoError(t, err)
	require.False(t, pending)
	require.Equal(t, 15, event.TotalTokens)
}

func TestZCodeUnknownSchemaIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.sqlite")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE model_usage (id TEXT)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	_, err = readZCodeUsagePage(context.Background(), path, 0, 1)
	require.Error(t, err)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestZCodeNormalizeRejectsInvalidAndDefersPending(t *testing.T) {
	row := zcodeUsageRow{ID: "id", SessionID: "session", ProviderID: "provider", ModelID: "model", Status: "completed", StartedAt: sql.NullInt64{Int64: time.Now().UnixMilli(), Valid: true}, CompletedAt: sql.NullInt64{Int64: time.Now().UnixMilli(), Valid: true}, Input: 10, Output: 5, Total: 15}
	row.Reasoning = 6
	_, _, err := normalizeZCodeUsage(row)
	require.Error(t, err)
	row.Reasoning = 0
	row.Input = 2147483648
	_, _, err = normalizeZCodeUsage(row)
	require.Error(t, err)
	row.Status = "running"
	_, pending, err := normalizeZCodeUsage(row)
	require.NoError(t, err)
	require.True(t, pending)
}
