package usagereporter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/glebarez/go-sqlite"
)

// zcodeUsageRow deliberately excludes messages, errors, raw usage and provider
// metadata. ProviderID is local route evidence, never part of UsageEvent.
type zcodeUsageRow struct {
	ID, SessionID, ProviderID, ModelID, Status             string
	CompletedAt                                            sql.NullInt64
	StartedAt                                              sql.NullInt64
	Input, Output, Reasoning, CacheWrite, CacheRead, Total int64
	InvalidNumeric                                         bool
}

// readZCodeUsagePage reads one short, consistent snapshot from an existing
// database. mode=ro observes an active WAL and never creates the source file.
func readZCodeUsagePage(ctx context.Context, path string, offset, limit int) ([]zcodeUsageRow, error) {
	if offset < 0 || limit < 1 || limit > 1000 {
		return nil, errors.New("invalid ZCode scan page")
	}
	path, err := ExpandPath(path)
	if err != nil {
		return nil, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, errors.New("invalid ZCode database path")
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("ZCode database is unavailable")
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	q := u.Query()
	q.Set("mode", "ro")
	q.Add("_pragma", "query_only(1)")
	q.Add("_pragma", "busy_timeout(2000)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, errors.New("could not open ZCode database")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, errors.New("could not read ZCode database")
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id, session_id, provider_id, model_id, status, started_at, completed_at,
		input_tokens, output_tokens, reasoning_tokens, cache_creation_input_tokens,
		cache_read_input_tokens, computed_total_tokens FROM model_usage ORDER BY id LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, errors.New("unsupported or unreadable ZCode usage schema")
	}
	defer rows.Close()
	result := make([]zcodeUsageRow, 0, limit)
	for rows.Next() {
		var row zcodeUsageRow
		var id, session, provider, model, status sql.NullString
		var started, completed, input, output, reasoning, cacheWrite, cacheRead, total any
		if err := rows.Scan(&id, &session, &provider, &model, &status, &started, &completed,
			&input, &output, &reasoning, &cacheWrite, &cacheRead, &total); err != nil {
			return nil, errors.New("invalid ZCode usage field type")
		}
		row.ID, row.SessionID, row.ProviderID, row.ModelID, row.Status = id.String, session.String, provider.String, model.String, status.String
		if started != nil {
			if value, ok := started.(int64); ok {
				row.StartedAt = sql.NullInt64{Int64: value, Valid: true}
			} else {
				row.InvalidNumeric = true
			}
		}
		if completed != nil {
			if value, ok := completed.(int64); ok {
				row.CompletedAt = sql.NullInt64{Int64: value, Valid: true}
			} else {
				row.InvalidNumeric = true
			}
		}
		for i, raw := range []any{input, output, reasoning, cacheWrite, cacheRead, total} {
			value, ok := raw.(int64)
			if !ok {
				row.InvalidNumeric = true
				continue
			}
			switch i {
			case 0:
				row.Input = value
			case 1:
				row.Output = value
			case 2:
				row.Reasoning = value
			case 3:
				row.CacheWrite = value
			case 4:
				row.CacheRead = value
			case 5:
				row.Total = value
			}
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("could not read ZCode usage rows")
	}
	return result, nil
}

// normalizeZCodeUsage returns pending for incomplete rows and rejects invalid
// quantities rather than saturating or pretending the row is complete.
func normalizeZCodeUsage(row zcodeUsageRow) (UsageEvent, bool, error) {
	if row.InvalidNumeric {
		return UsageEvent{}, false, errors.New("invalid ZCode numeric field type")
	}
	if strings.TrimSpace(row.ID) == "" || strings.TrimSpace(row.SessionID) == "" || strings.TrimSpace(row.ProviderID) == "" || strings.TrimSpace(row.ModelID) == "" {
		return UsageEvent{}, true, nil
	}
	if strings.ContainsRune(row.ID, '\x00') || strings.ContainsRune(row.SessionID, '\x00') {
		return UsageEvent{}, false, errors.New("invalid ZCode usage identity")
	}
	if row.Status != "completed" && row.Status != "error" && row.Status != "cancelled" || !row.StartedAt.Valid || row.StartedAt.Int64 <= 0 || !row.CompletedAt.Valid || row.CompletedAt.Int64 <= 0 {
		return UsageEvent{}, true, nil
	}
	if row.StartedAt.Int64 > row.CompletedAt.Int64 {
		return UsageEvent{}, false, errors.New("invalid ZCode usage time range")
	}
	for _, n := range []int64{row.Input, row.Output, row.Reasoning, row.CacheWrite, row.CacheRead, row.Total} {
		if n < 0 || n > math.MaxInt32 {
			return UsageEvent{}, false, errors.New("invalid ZCode token count")
		}
	}
	if row.Input+row.Output != row.Total || row.CacheRead+row.CacheWrite > row.Input || row.Reasoning > row.Output {
		return UsageEvent{}, false, errors.New("inconsistent ZCode token counts")
	}
	if row.Total == 0 {
		return UsageEvent{}, false, nil
	}
	return UsageEvent{
		EventID:   sha256Hex(fmt.Sprintf("zcode:model_usage:v1\x00%s\x00%s", row.SessionID, row.ID)),
		SessionID: sha256Hex("zcode:session:v1\x00" + row.SessionID), OccurredAt: row.CompletedAt.Int64 / 1000,
		SourceModelName: row.ModelID, InputTokens: int(row.Input), CachedInputTokens: int(row.CacheRead),
		OutputTokens: int(row.Output), ReasoningOutputTokens: int(row.Reasoning), TotalTokens: int(row.Total),
	}, false, nil
}
